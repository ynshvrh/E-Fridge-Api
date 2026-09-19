package fridge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
)

var (
	ErrFridgeNotFound = errors.New("fridge not found")
	ErrNotAuthorized  = errors.New("not authorized to perform this action")
	ErrMemberNotFound = errors.New("user with this email not found")
)

type Service struct {
	queries *db.Queries
}

func NewService(queries *db.Queries) *Service {
	return &Service{queries: queries}
}

type FridgeDTO struct {
	ID        uuid.UUID   `json:"id"`
	Name      string      `json:"name"`
	OwnerID   uuid.UUID   `json:"owner_id"`
	Role      string      `json:"role"`
	CreatedAt time.Time   `json:"created_at"`
	Members   []MemberDTO `json:"members,omitempty"`
}

type MemberDTO struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

func (s *Service) GetMyFridges(ctx context.Context, userID uuid.UUID) ([]FridgeDTO, error) {
	rows, err := s.queries.GetFridgesByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get fridges: %w", err)
	}

	result := make([]FridgeDTO, 0, len(rows))
	for _, r := range rows {
		result = append(result, FridgeDTO{
			ID:        r.ID,
			Name:      r.Name,
			OwnerID:   r.OwnerID,
			Role:      r.Role,
			CreatedAt: r.CreatedAt,
		})
	}
	return result, nil
}

func (s *Service) CreateFridge(ctx context.Context, userID uuid.UUID, name string) (*FridgeDTO, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Новий холодильник"
	}

	f, err := s.queries.CreateFridge(ctx, db.CreateFridgeParams{
		Name:    name,
		OwnerID: userID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create fridge: %w", err)
	}

	_, err = s.queries.AddFridgeMember(ctx, db.AddFridgeMemberParams{
		FridgeID: f.ID,
		UserID:   userID,
		Role:     "owner",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to add fridge owner member: %w", err)
	}

	return &FridgeDTO{
		ID:        f.ID,
		Name:      f.Name,
		OwnerID:   f.OwnerID,
		Role:      "owner",
		CreatedAt: f.CreatedAt,
	}, nil
}

func (s *Service) GetFridgeDetails(ctx context.Context, fridgeID, userID uuid.UUID) (*FridgeDTO, error) {
	member, err := s.queries.GetFridgeMember(ctx, db.GetFridgeMemberParams{
		FridgeID: fridgeID,
		UserID:   userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotAuthorized
		}
		return nil, err
	}

	f, err := s.queries.GetFridgeByID(ctx, fridgeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrFridgeNotFound
		}
		return nil, err
	}

	members, err := s.queries.GetFridgeMembers(ctx, fridgeID)
	if err != nil {
		return nil, err
	}

	memberDTOs := make([]MemberDTO, 0, len(members))
	for _, m := range members {
		memberDTOs = append(memberDTOs, MemberDTO{
			ID:       m.ID,
			Name:     m.Name,
			Email:    m.Email,
			Role:     m.Role,
			JoinedAt: m.JoinedAt,
		})
	}

	return &FridgeDTO{
		ID:        f.ID,
		Name:      f.Name,
		OwnerID:   f.OwnerID,
		Role:      member.Role,
		CreatedAt: f.CreatedAt,
		Members:   memberDTOs,
	}, nil
}

func (s *Service) AddMemberByEmail(ctx context.Context, fridgeID, actorID uuid.UUID, email, role string) (*MemberDTO, error) {
	actorMember, err := s.queries.GetFridgeMember(ctx, db.GetFridgeMemberParams{
		FridgeID: fridgeID,
		UserID:   actorID,
	})
	if err != nil || (actorMember.Role != "owner" && actorMember.Role != "admin") {
		return nil, ErrNotAuthorized
	}

	targetUser, err := s.queries.GetUserByEmail(ctx, strings.TrimSpace(email))
	if err != nil {
		return nil, ErrMemberNotFound
	}

	if role == "" {
		role = "member"
	}

	m, err := s.queries.AddFridgeMember(ctx, db.AddFridgeMemberParams{
		FridgeID: fridgeID,
		UserID:   targetUser.ID,
		Role:     role,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to add member: %w", err)
	}

	return &MemberDTO{
		ID:       targetUser.ID,
		Name:     targetUser.Name,
		Email:    targetUser.Email,
		Role:     m.Role,
		JoinedAt: m.JoinedAt,
	}, nil
}

func (s *Service) RemoveMember(ctx context.Context, fridgeID, actorID, targetUserID uuid.UUID) error {
	actorMember, err := s.queries.GetFridgeMember(ctx, db.GetFridgeMemberParams{
		FridgeID: fridgeID,
		UserID:   actorID,
	})
	if err != nil || (actorMember.Role != "owner" && actorMember.Role != "admin") {
		return ErrNotAuthorized
	}

	return s.queries.RemoveFridgeMember(ctx, db.RemoveFridgeMemberParams{
		FridgeID: fridgeID,
		UserID:   targetUserID,
	})
}
