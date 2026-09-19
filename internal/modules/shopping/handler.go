package shopping

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

func (h *Handler) Routes(jwtSecret string, queries *db.Queries) chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.Auth(jwtSecret))

	// All shopping routes require a fridge context
	r.Group(func(fridgeRouter chi.Router) {
		fridgeRouter.Use(middleware.RequireFridge(queries))

		fridgeRouter.Get("/", h.List)
		fridgeRouter.Post("/", h.Create)
		fridgeRouter.Post("/batch", h.BatchCreate)
		fridgeRouter.Delete("/", h.ClearAll)
		fridgeRouter.Delete("/bought", h.ClearBought)

		fridgeRouter.Put("/{id}", h.Update)
		fridgeRouter.Patch("/{id}/toggle", h.ToggleBought)
		fridgeRouter.Post("/{id}/purchase", h.Purchase)
		fridgeRouter.Delete("/{id}", h.Delete)
	})

	return r
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	fridgeID, ok := middleware.GetFridgeID(r.Context())
	if !ok {
		response.Error(w, http.StatusBadRequest, "Missing fridge context", "BAD_REQUEST")
		return
	}

	summary, err := h.service.ListItems(r.Context(), fridgeID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error(), "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, summary)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	fridgeID, ok := middleware.GetFridgeID(r.Context())
	if !ok {
		response.Error(w, http.StatusBadRequest, "Missing fridge context", "BAD_REQUEST")
		return
	}
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Error(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return
	}

	var input CreateItemInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST")
		return
	}

	item, err := h.service.CreateItem(r.Context(), fridgeID, userID, input)
	if err != nil {
		if errors.Is(err, ErrEmptyName) || errors.Is(err, ErrInvalidQuantity) {
			response.Error(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error(), "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusCreated, item)
}

func (h *Handler) BatchCreate(w http.ResponseWriter, r *http.Request) {
	fridgeID, ok := middleware.GetFridgeID(r.Context())
	if !ok {
		response.Error(w, http.StatusBadRequest, "Missing fridge context", "BAD_REQUEST")
		return
	}
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Error(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return
	}

	var req struct {
		Items []CreateItemInput `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST")
		return
	}

	items, err := h.service.BatchCreateItems(r.Context(), fridgeID, userID, req.Items)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error(), "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusCreated, map[string]any{
		"items": items,
		"count": len(items),
	})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	fridgeID, ok := middleware.GetFridgeID(r.Context())
	if !ok {
		response.Error(w, http.StatusBadRequest, "Missing fridge context", "BAD_REQUEST")
		return
	}

	idStr := chi.URLParam(r, "id")
	itemID, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid item ID", "BAD_REQUEST")
		return
	}

	var input UpdateItemInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST")
		return
	}

	item, err := h.service.UpdateItem(r.Context(), fridgeID, itemID, input)
	if err != nil {
		if errors.Is(err, ErrItemNotFound) {
			response.Error(w, http.StatusNotFound, "Shopping item not found", "NOT_FOUND")
			return
		}
		if errors.Is(err, ErrEmptyName) || errors.Is(err, ErrInvalidQuantity) {
			response.Error(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error(), "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, item)
}

func (h *Handler) ToggleBought(w http.ResponseWriter, r *http.Request) {
	fridgeID, ok := middleware.GetFridgeID(r.Context())
	if !ok {
		response.Error(w, http.StatusBadRequest, "Missing fridge context", "BAD_REQUEST")
		return
	}

	idStr := chi.URLParam(r, "id")
	itemID, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid item ID", "BAD_REQUEST")
		return
	}

	var req struct {
		IsBought bool `json:"is_bought"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST")
		return
	}

	item, err := h.service.ToggleItemBought(r.Context(), fridgeID, itemID, req.IsBought)
	if err != nil {
		if errors.Is(err, ErrItemNotFound) {
			response.Error(w, http.StatusNotFound, "Shopping item not found", "NOT_FOUND")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error(), "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, item)
}

func (h *Handler) Purchase(w http.ResponseWriter, r *http.Request) {
	fridgeID, ok := middleware.GetFridgeID(r.Context())
	if !ok {
		response.Error(w, http.StatusBadRequest, "Missing fridge context", "BAD_REQUEST")
		return
	}
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Error(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return
	}

	idStr := chi.URLParam(r, "id")
	itemID, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid item ID", "BAD_REQUEST")
		return
	}

	var input PurchaseItemInput
	// Request body is optional
	_ = json.NewDecoder(r.Body).Decode(&input)

	product, err := h.service.PurchaseAndMoveToFridge(r.Context(), fridgeID, userID, itemID, input)
	if err != nil {
		if errors.Is(err, ErrItemNotFound) {
			response.Error(w, http.StatusNotFound, "Shopping item not found", "NOT_FOUND")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error(), "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, map[string]any{
		"message": "Item moved to fridge successfully",
		"product": product,
	})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	fridgeID, ok := middleware.GetFridgeID(r.Context())
	if !ok {
		response.Error(w, http.StatusBadRequest, "Missing fridge context", "BAD_REQUEST")
		return
	}

	idStr := chi.URLParam(r, "id")
	itemID, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid item ID", "BAD_REQUEST")
		return
	}

	if err := h.service.DeleteItem(r.Context(), fridgeID, itemID); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error(), "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "Item deleted successfully"})
}

func (h *Handler) ClearBought(w http.ResponseWriter, r *http.Request) {
	fridgeID, ok := middleware.GetFridgeID(r.Context())
	if !ok {
		response.Error(w, http.StatusBadRequest, "Missing fridge context", "BAD_REQUEST")
		return
	}

	if err := h.service.ClearBoughtItems(r.Context(), fridgeID); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error(), "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "Bought items cleared"})
}

func (h *Handler) ClearAll(w http.ResponseWriter, r *http.Request) {
	fridgeID, ok := middleware.GetFridgeID(r.Context())
	if !ok {
		response.Error(w, http.StatusBadRequest, "Missing fridge context", "BAD_REQUEST")
		return
	}

	if err := h.service.ClearAllItems(r.Context(), fridgeID); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error(), "INTERNAL_ERROR")
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "Shopping list cleared"})
}
