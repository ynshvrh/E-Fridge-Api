package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/ynshvrh/E-Fridge-Api/internal/config"
	"github.com/ynshvrh/E-Fridge-Api/internal/database"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
	"github.com/ynshvrh/E-Fridge-Api/internal/middleware"
	"github.com/ynshvrh/E-Fridge-Api/internal/modules/auth"
	"github.com/ynshvrh/E-Fridge-Api/internal/modules/chef"
	"github.com/ynshvrh/E-Fridge-Api/internal/modules/cooking"
	"github.com/ynshvrh/E-Fridge-Api/internal/modules/fridge"
	"github.com/ynshvrh/E-Fridge-Api/internal/modules/nutrition"
	"github.com/ynshvrh/E-Fridge-Api/internal/modules/planner"
	"github.com/ynshvrh/E-Fridge-Api/internal/modules/products"
	"github.com/ynshvrh/E-Fridge-Api/internal/modules/recipes"
	"github.com/ynshvrh/E-Fridge-Api/internal/modules/shopping"
	"github.com/ynshvrh/E-Fridge-Api/internal/pkg/response"
)

func main() {
	// 1. Logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// 2. Config
	cfg := config.Load()
	slog.Info("Starting E-Fridge API", "port", cfg.Port, "env", cfg.Environment)

	// 3. Database
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	dbConn, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("Failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer dbConn.Close()

	queries := db.New(dbConn.Pool)

	// 4. Services & Handlers
	authService := auth.NewService(queries, cfg, dbConn.Pool)
	authHandler := auth.NewHandler(authService)

	fridgeService := fridge.NewService(queries)
	fridgeHandler := fridge.NewHandler(fridgeService)

	productsService := products.NewService(queries, cfg)
	productsHandler := products.NewHandler(productsService)

	nutritionService := nutrition.NewService(queries)
	nutritionHandler := nutrition.NewHandler(nutritionService)

	cookingService := cooking.NewService(queries, productsService, nutritionService)
	cookingHandler := cooking.NewHandler(cookingService)

	// Rate limiter & concurrency guard for AI endpoints (15s cooldown, max 10/5min)
	aiGuard := middleware.NewAIGuard(15*time.Second, 10, 5*time.Minute)

	chefService := chef.NewService(queries, cfg)
	chefHandler := chef.NewHandler(chefService, aiGuard)

	shoppingService := shopping.NewService(queries, productsService)
	shoppingHandler := shopping.NewHandler(shoppingService)

	recipesService := recipes.NewService(queries)
	recipesHandler := recipes.NewHandler(recipesService)

	plannerService := planner.NewService(queries, cfg)
	plannerHandler := planner.NewHandler(plannerService, aiGuard)

	// Periodic cleaner for expired pending registrations (every 1 hour)
	cleanerStop := make(chan struct{})
	go func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := authService.CleanExpiredPendingRegistrations(cleanCtx); err != nil {
			slog.Warn("Initial cleanup of expired pending registrations failed", "error", err)
		} else {
			slog.Info("Completed initial cleanup of expired pending registrations")
		}
		cleanCancel()

		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-cleanerStop:
				return
			case <-ticker.C:
				cCtx, cCancel := context.WithTimeout(context.Background(), 10*time.Second)
				if err := authService.CleanExpiredPendingRegistrations(cCtx); err != nil {
					slog.Warn("Failed to clean expired pending registrations", "error", err)
				} else {
					slog.Info("Successfully cleaned expired pending registrations")
				}
				cCancel()
			}
		}
	}()

	// 5. Router
	r := chi.NewRouter()

	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(middleware.MaxBodySize(1 << 20)) // 1 MB request body limit

	// CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "X-Fridge-Id"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health Check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		response.JSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "E-Fridge-Api"})
	})

	// API v1 Routes
	r.Route("/api/v1", func(api chi.Router) {
		api.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			response.JSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "E-Fridge-Api v1"})
		})

		api.Mount("/auth", authHandler.Routes(cfg.JWTSecret))
		api.Mount("/fridges", fridgeHandler.Routes(cfg.JWTSecret))
		api.Mount("/products", productsHandler.Routes(cfg.JWTSecret, queries))
		api.Mount("/nutrition", nutritionHandler.Routes(cfg.JWTSecret))
		api.Mount("/cooking", cookingHandler.Routes(cfg.JWTSecret, queries))
		api.Mount("/chef", chefHandler.Routes(cfg.JWTSecret, queries))
		api.Mount("/shopping", shoppingHandler.Routes(cfg.JWTSecret, queries))
		api.Mount("/recipes", recipesHandler.Routes(cfg.JWTSecret))
		api.Mount("/planner", plannerHandler.Routes(cfg.JWTSecret, queries))
	})

	// 6. HTTP Server
	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown channel
	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info(fmt.Sprintf("Server listening on http://localhost:%s", cfg.Port))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-shutdownChan
	slog.Info("Shutting down server gracefully...")

	close(cleanerStop)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
	}

	slog.Info("Server stopped cleanly")
}
