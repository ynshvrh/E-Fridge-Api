package products

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
	"github.com/ynshvrh/E-Fridge-Api/internal/middleware"
	"github.com/ynshvrh/E-Fridge-Api/internal/pkg/response"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

type ConsumeRequest struct {
	Amount float64 `json:"amount"`
}

func (h *Handler) Routes(jwtSecret string, queries *db.Queries) chi.Router {
	r := chi.NewRouter()

	// Categories metadata does not require authentication
	r.Get("/categories", h.Categories)

	// Protected product routes
	r.Group(func(authRouter chi.Router) {
		authRouter.Use(middleware.Auth(jwtSecret))

		authRouter.Post("/estimate-nutrition", h.EstimateNutrition)
		authRouter.Get("/barcode/{code}", h.LookupBarcode)

		// All following routes require a valid fridge context (X-Fridge-Id header)
		authRouter.Group(func(fridgeRouter chi.Router) {
			fridgeRouter.Use(middleware.RequireFridge(queries))

			fridgeRouter.Get("/", h.List)
			fridgeRouter.Post("/", h.Create)
			fridgeRouter.Delete("/", h.Clear)

			fridgeRouter.Get("/{id}", h.Get)
			fridgeRouter.Put("/{id}", h.Update)
			fridgeRouter.Delete("/{id}", h.Delete)
			fridgeRouter.Post("/{id}/consume", h.Consume)
		})
	})

	return r
}

func (h *Handler) Categories(w http.ResponseWriter, r *http.Request) {
	categories := h.service.GetCategories()
	response.JSON(w, http.StatusOK, categories)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	fridgeID, ok := middleware.GetFridgeID(r.Context())
	if !ok {
		response.Error(w, http.StatusBadRequest, "Missing fridge context", "BAD_REQUEST")
		return
	}

	category := r.URL.Query().Get("category")
	search := r.URL.Query().Get("search")

	list, err := h.service.ListProducts(r.Context(), fridgeID, category, search)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to list products", "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, list)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	fridgeID, _ := middleware.GetFridgeID(r.Context())
	userID, _ := middleware.GetUserID(r.Context())

	var input CreateProductInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request payload", "INVALID_REQUEST")
		return
	}

	product, err := h.service.CreateProduct(r.Context(), fridgeID, userID, input)
	if err != nil {
		if errors.Is(err, ErrEmptyName) || errors.Is(err, ErrInvalidQuantity) {
			response.Error(w, http.StatusBadRequest, err.Error(), "INVALID_INPUT")
			return
		}
		response.Error(w, http.StatusInternalServerError, "Failed to create product", "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusCreated, product)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	fridgeID, _ := middleware.GetFridgeID(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid product ID", "INVALID_ID")
		return
	}

	product, err := h.service.GetProduct(r.Context(), fridgeID, id)
	if err != nil {
		if errors.Is(err, ErrProductNotFound) {
			response.Error(w, http.StatusNotFound, "Product not found", "NOT_FOUND")
			return
		}
		response.Error(w, http.StatusInternalServerError, "Failed to get product", "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, product)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	fridgeID, _ := middleware.GetFridgeID(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid product ID", "INVALID_ID")
		return
	}

	var input UpdateProductInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request payload", "INVALID_REQUEST")
		return
	}

	product, err := h.service.UpdateProduct(r.Context(), fridgeID, id, input)
	if err != nil {
		if errors.Is(err, ErrProductNotFound) {
			response.Error(w, http.StatusNotFound, "Product not found", "NOT_FOUND")
			return
		}
		if errors.Is(err, ErrEmptyName) || errors.Is(err, ErrInvalidQuantity) {
			response.Error(w, http.StatusBadRequest, err.Error(), "INVALID_INPUT")
			return
		}
		response.Error(w, http.StatusInternalServerError, "Failed to update product", "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, product)
}

func (h *Handler) Consume(w http.ResponseWriter, r *http.Request) {
	fridgeID, _ := middleware.GetFridgeID(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid product ID", "INVALID_ID")
		return
	}

	var req ConsumeRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	product, err := h.service.ConsumeProduct(r.Context(), fridgeID, id, req.Amount)
	if err != nil {
		if errors.Is(err, ErrProductNotFound) {
			response.Error(w, http.StatusNotFound, "Product not found", "NOT_FOUND")
			return
		}
		response.Error(w, http.StatusInternalServerError, "Failed to consume product", "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, product)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	fridgeID, _ := middleware.GetFridgeID(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid product ID", "INVALID_ID")
		return
	}

	if err := h.service.DeleteProduct(r.Context(), fridgeID, id); err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to delete product", "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "Product deleted successfully"})
}

func (h *Handler) Clear(w http.ResponseWriter, r *http.Request) {
	fridgeID, _ := middleware.GetFridgeID(r.Context())

	if err := h.service.ClearFridge(r.Context(), fridgeID); err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to clear fridge", "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "Fridge cleared successfully"})
}

func (h *Handler) EstimateNutrition(w http.ResponseWriter, r *http.Request) {
	var input EstimateNutritionInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request payload", "INVALID_REQUEST")
		return
	}

	res, err := h.service.EstimateNutrition(r.Context(), input)
	if err != nil {
		if errors.Is(err, ErrEmptyName) {
			response.Error(w, http.StatusBadRequest, "Product name cannot be empty", "INVALID_INPUT")
			return
		}
		response.Error(w, http.StatusInternalServerError, "Failed to estimate nutrition", "ESTIMATION_FAILED")
		return
	}

	response.JSON(w, http.StatusOK, res)
}

func (h *Handler) LookupBarcode(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	if code == "" {
		response.Error(w, http.StatusBadRequest, "Barcode is required", "INVALID_INPUT")
		return
	}

	res, err := h.service.LookupBarcode(r.Context(), code)
	if err != nil {
		if errors.Is(err, ErrBarcodeNotFound) {
			response.Error(w, http.StatusNotFound, "Товар за цим штрих-кодом не знайдено в базі OpenFoodFacts", "NOT_FOUND")
			return
		}
		if errors.Is(err, ErrInvalidBarcode) {
			response.Error(w, http.StatusBadRequest, "Некоректний формат штрих-коду", "INVALID_BARCODE")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error(), "LOOKUP_FAILED")
		return
	}

	response.JSON(w, http.StatusOK, res)
}

