package fridge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ynshvrh/E-Fridge-Api/internal/database"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
)

var (
	ErrFridgeNotFound            = errors.New("fridge not found")
	ErrNotAuthorized             = errors.New("not authorized to perform this action")
	ErrMemberNotFound            = errors.New("user with this email not found")
	ErrInvalidRole               = errors.New("invalid member role")
	ErrCannotRemoveOwner         = errors.New("cannot remove fridge owner")
	ErrInviteNotFoundOrExpired   = errors.New("invite not found or has expired")
	ErrOwnerMustTransferOrDelete = errors.New("owner cannot leave while other members exist; transfer ownership or delete fridge")
	ErrCannotTransferToSelf      = errors.New("cannot transfer ownership to yourself")
)

type Service struct {
	queries *db.Queries
	pool    *pgxpool.Pool
}

func NewService(queries *db.Queries, pool ...*pgxpool.Pool) *Service {
	s := &Service{queries: queries}
	if len(pool) > 0 {
		s.pool = pool[0]
	}
	return s
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

type InviteDTO struct {
	ID        uuid.UUID `json:"id"`
	FridgeID  uuid.UUID `json:"fridge_id"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type InviteDetailsDTO struct {
	FridgeID   uuid.UUID `json:"fridge_id"`
	FridgeName string    `json:"fridge_name"`
	Token      string    `json:"token"`
	ExpiresAt  time.Time `json:"expires_at"`
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

	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" {
		role = "member"
	}

	// Role whitelist: only "member" and "admin" are allowed. Role "owner" cannot be assigned via AddMember!
	if role != "member" && role != "admin" {
		return nil, fmt.Errorf("%w: role must be 'member' or 'admin'", ErrInvalidRole)
	}

	targetUser, err := s.queries.GetUserByEmail(ctx, strings.TrimSpace(email))
	if err != nil {
		return nil, ErrMemberNotFound
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

	targetMember, err := s.queries.GetFridgeMember(ctx, db.GetFridgeMemberParams{
		FridgeID: fridgeID,
		UserID:   targetUserID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMemberNotFound
		}
		return fmt.Errorf("failed to find member: %w", err)
	}

	// Protection: Fridge owner cannot be removed via RemoveMember!
	if targetMember.Role == "owner" {
		return ErrCannotRemoveOwner
	}

	// Protection: Admin cannot remove another admin (only owner can remove admins)
	if actorMember.Role == "admin" && targetMember.Role == "admin" && actorID != targetUserID {
		return ErrNotAuthorized
	}

	return s.queries.RemoveFridgeMember(ctx, db.RemoveFridgeMemberParams{
		FridgeID: fridgeID,
		UserID:   targetUserID,
	})
}

func (s *Service) CreateInvite(ctx context.Context, fridgeID, actorID uuid.UUID) (*InviteDTO, error) {
	actorMember, err := s.queries.GetFridgeMember(ctx, db.GetFridgeMemberParams{
		FridgeID: fridgeID,
		UserID:   actorID,
	})
	if err != nil || (actorMember.Role != "owner" && actorMember.Role != "admin") {
		return nil, ErrNotAuthorized
	}

	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("failed to generate invite token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)
	expiresAt := time.Now().Add(7 * 24 * time.Hour) // valid for 7 days

	inv, err := s.queries.CreateFridgeInvite(ctx, db.CreateFridgeInviteParams{
		FridgeID:  fridgeID,
		Token:     token,
		CreatedBy: pgtype.UUID{Bytes: actorID, Valid: true},
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create invite: %w", err)
	}

	return &InviteDTO{
		ID:        inv.ID,
		FridgeID:  inv.FridgeID,
		Token:     inv.Token,
		ExpiresAt: inv.ExpiresAt,
		CreatedAt: inv.CreatedAt,
	}, nil
}

func (s *Service) GetInviteDetails(ctx context.Context, token string) (*InviteDetailsDTO, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrInviteNotFoundOrExpired
	}

	inv, err := s.queries.GetFridgeInviteByToken(ctx, token)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInviteNotFoundOrExpired
		}
		return nil, fmt.Errorf("failed to get invite: %w", err)
	}

	return &InviteDetailsDTO{
		FridgeID:   inv.FridgeID,
		FridgeName: inv.FridgeName,
		Token:      inv.Token,
		ExpiresAt:  inv.ExpiresAt,
	}, nil
}

func (s *Service) JoinFridge(ctx context.Context, token string, userID uuid.UUID) (*FridgeDTO, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrInviteNotFoundOrExpired
	}

	inv, err := s.queries.GetFridgeInviteByToken(ctx, token)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInviteNotFoundOrExpired
		}
		return nil, fmt.Errorf("failed to get invite: %w", err)
	}

	// Add member with role "member" (ON CONFLICT DO NOTHING ensures safety if already joined)
	_, err = s.queries.AddFridgeMember(ctx, db.AddFridgeMemberParams{
		FridgeID: inv.FridgeID,
		UserID:   userID,
		Role:     "member",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to join fridge: %w", err)
	}

	return s.GetFridgeDetails(ctx, inv.FridgeID, userID)
}

func (s *Service) LeaveFridge(ctx context.Context, fridgeID, userID uuid.UUID) error {
	member, err := s.queries.GetFridgeMember(ctx, db.GetFridgeMemberParams{
		FridgeID: fridgeID,
		UserID:   userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotAuthorized
		}
		return err
	}

	if member.Role == "owner" {
		count, err := s.queries.CountFridgeMembers(ctx, fridgeID)
		if err != nil {
			return fmt.Errorf("failed to count fridge members: %w", err)
		}
		if count > 1 {
			return ErrOwnerMustTransferOrDelete
		}
		// Sole owner leaves: delete the fridge
		return s.queries.DeleteFridge(ctx, fridgeID)
	}

	return s.queries.RemoveFridgeMember(ctx, db.RemoveFridgeMemberParams{
		FridgeID: fridgeID,
		UserID:   userID,
	})
}

func (s *Service) TransferOwnership(ctx context.Context, fridgeID, currentOwnerID, newOwnerID uuid.UUID) error {
	if currentOwnerID == newOwnerID {
		return ErrCannotTransferToSelf
	}

	ownerMember, err := s.queries.GetFridgeMember(ctx, db.GetFridgeMemberParams{
		FridgeID: fridgeID,
		UserID:   currentOwnerID,
	})
	if err != nil || ownerMember.Role != "owner" {
		return ErrNotAuthorized
	}

	_, err = s.queries.GetFridgeMember(ctx, db.GetFridgeMemberParams{
		FridgeID: fridgeID,
		UserID:   newOwnerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMemberNotFound
		}
		return fmt.Errorf("failed to find target member: %w", err)
	}

	if s.pool != nil {
		return database.WithTransaction(ctx, s.pool, func(qtx *db.Queries) error {
			if err := qtx.TransferFridgeOwnership(ctx, db.TransferFridgeOwnershipParams{
				ID:      fridgeID,
				OwnerID: newOwnerID,
			}); err != nil {
				return err
			}
			if err := qtx.UpdateFridgeMemberRole(ctx, db.UpdateFridgeMemberRoleParams{
				FridgeID: fridgeID,
				UserID:   newOwnerID,
				Role:     "owner",
			}); err != nil {
				return err
			}
			return qtx.UpdateFridgeMemberRole(ctx, db.UpdateFridgeMemberRoleParams{
				FridgeID: fridgeID,
				UserID:   currentOwnerID,
				Role:     "admin",
			})
		})
	}

	// Sequential fallback
	if err := s.queries.TransferFridgeOwnership(ctx, db.TransferFridgeOwnershipParams{
		ID:      fridgeID,
		OwnerID: newOwnerID,
	}); err != nil {
		return err
	}
	if err := s.queries.UpdateFridgeMemberRole(ctx, db.UpdateFridgeMemberRoleParams{
		FridgeID: fridgeID,
		UserID:   newOwnerID,
		Role:     "owner",
	}); err != nil {
		return err
	}
	return s.queries.UpdateFridgeMemberRole(ctx, db.UpdateFridgeMemberRoleParams{
		FridgeID: fridgeID,
		UserID:   currentOwnerID,
		Role:     "admin",
	})
}
