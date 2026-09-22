package fridge

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/ynshvrh/E-Fridge-Api/internal/database"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
)

func TestFridgeRoleSecurityWithDB(t *testing.T) {
	ctx := context.Background()
	dbURL := "postgres://postgres:postgrespassword@localhost:5432/e_fridge?sslmode=disable"

	dbConn, err := database.Connect(ctx, dbURL)
	if err != nil {
		t.Skip("skipping DB integration test, cannot connect to PostgreSQL")
		return
	}
	defer dbConn.Close()

	queries := db.New(dbConn.Pool)
	service := NewService(queries)

	// Create test users: owner, admin, member, target
	unique := time.Now().UnixNano()
	ownerUser, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email:        fmt.Sprintf("owner_%d@example.com", unique),
		Name:         "Owner User",
		PasswordHash: "dummyhash",
	})
	if err != nil {
		t.Fatalf("failed to create owner: %v", err)
	}

	adminUser, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email:        fmt.Sprintf("admin_%d@example.com", unique),
		Name:         "Admin User",
		PasswordHash: "dummyhash",
	})
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	memberUser, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email:        fmt.Sprintf("member_%d@example.com", unique),
		Name:         "Member User",
		PasswordHash: "dummyhash",
	})
	if err != nil {
		t.Fatalf("failed to create member: %v", err)
	}

	targetUser, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email:        fmt.Sprintf("target_%d@example.com", unique),
		Name:         "Target User",
		PasswordHash: "dummyhash",
	})
	if err != nil {
		t.Fatalf("failed to create target: %v", err)
	}

	// Create fridge
	fridge, err := service.CreateFridge(ctx, ownerUser.ID, "Security Test Fridge")
	if err != nil {
		t.Fatalf("failed to create fridge: %v", err)
	}

	// Add admin to fridge
	_, err = service.AddMemberByEmail(ctx, fridge.ID, ownerUser.ID, adminUser.Email, "admin")
	if err != nil {
		t.Fatalf("failed to add admin: %v", err)
	}

	// Add member to fridge
	_, err = service.AddMemberByEmail(ctx, fridge.ID, ownerUser.ID, memberUser.Email, "member")
	if err != nil {
		t.Fatalf("failed to add member: %v", err)
	}

	// 1. Role Whitelist Test: Cannot add member with role "owner"
	_, err = service.AddMemberByEmail(ctx, fridge.ID, ownerUser.ID, targetUser.Email, "owner")
	if !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("expected ErrInvalidRole when assigning owner role, got: %v", err)
	}

	// 2. Role Whitelist Test: Cannot add member with invalid role
	_, err = service.AddMemberByEmail(ctx, fridge.ID, ownerUser.ID, targetUser.Email, "hacker_role")
	if !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("expected ErrInvalidRole for arbitrary role, got: %v", err)
	}

	// 3. Protection Test: Admin cannot remove the owner
	err = service.RemoveMember(ctx, fridge.ID, adminUser.ID, ownerUser.ID)
	if !errors.Is(err, ErrCannotRemoveOwner) {
		t.Fatalf("expected ErrCannotRemoveOwner when admin removes owner, got: %v", err)
	}

	// 4. Protection Test: Owner cannot be removed via RemoveMember
	err = service.RemoveMember(ctx, fridge.ID, ownerUser.ID, ownerUser.ID)
	if !errors.Is(err, ErrCannotRemoveOwner) {
		t.Fatalf("expected ErrCannotRemoveOwner when owner attempts to remove self, got: %v", err)
	}

	// 5. Protection Test: Admin cannot remove another admin
	secondAdmin, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email:        fmt.Sprintf("admin2_%d@example.com", unique),
		Name:         "Second Admin",
		PasswordHash: "dummyhash",
	})
	if err != nil {
		t.Fatalf("failed to create second admin: %v", err)
	}
	_, err = service.AddMemberByEmail(ctx, fridge.ID, ownerUser.ID, secondAdmin.Email, "admin")
	if err != nil {
		t.Fatalf("failed to add second admin: %v", err)
	}

	err = service.RemoveMember(ctx, fridge.ID, adminUser.ID, secondAdmin.ID)
	if !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("expected ErrNotAuthorized when admin removes another admin, got: %v", err)
	}

	// 6. Allowed: Owner can remove admin
	err = service.RemoveMember(ctx, fridge.ID, ownerUser.ID, secondAdmin.ID)
	if err != nil {
		t.Fatalf("expected owner to be able to remove admin, got: %v", err)
	}

	// 7. Allowed: Admin can remove regular member
	err = service.RemoveMember(ctx, fridge.ID, adminUser.ID, memberUser.ID)
	if err != nil {
		t.Fatalf("expected admin to be able to remove member, got: %v", err)
	}
}

func TestFridgePreserveProductsOnUserDeletionWithDB(t *testing.T) {
	ctx := context.Background()
	dbURL := "postgres://postgres:postgrespassword@localhost:5432/e_fridge?sslmode=disable"

	dbConn, err := database.Connect(ctx, dbURL)
	if err != nil {
		t.Skip("skipping DB integration test, cannot connect to PostgreSQL")
		return
	}
	defer dbConn.Close()

	queries := db.New(dbConn.Pool)
	service := NewService(queries, dbConn.Pool)

	unique := time.Now().UnixNano()
	ownerUser, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email:        fmt.Sprintf("f_owner_%d@example.com", unique),
		Name:         "Fridge Owner",
		PasswordHash: "dummyhash",
	})
	if err != nil {
		t.Fatalf("failed to create owner: %v", err)
	}

	memberUser, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email:        fmt.Sprintf("f_member_%d@example.com", unique),
		Name:         "Fridge Member",
		PasswordHash: "dummyhash",
	})
	if err != nil {
		t.Fatalf("failed to create member: %v", err)
	}

	fridge, err := service.CreateFridge(ctx, ownerUser.ID, "Family Fridge")
	if err != nil {
		t.Fatalf("failed to create fridge: %v", err)
	}

	// Member creates a product in this shared fridge
	product, err := queries.CreateProduct(ctx, db.CreateProductParams{
		FridgeID:  fridge.ID,
		Name:      "Shared Family Milk",
		Category:  "dairy",
		Quantity:  1.0,
		Unit:      "л",
		CreatedBy: pgtype.UUID{Bytes: memberUser.ID, Valid: true},
	})
	if err != nil {
		t.Fatalf("failed to create product: %v", err)
	}

	// Delete the member's account
	err = queries.DeleteUser(ctx, memberUser.ID)
	if err != nil {
		t.Fatalf("failed to delete member user: %v", err)
	}

	// Verify that the product is PRESERVED in the fridge, and created_by is SET NULL
	preserved, err := queries.GetProductByID(ctx, db.GetProductByIDParams{
		ID:       product.ID,
		FridgeID: fridge.ID,
	})
	if err != nil {
		t.Fatalf("CRITICAL BUG: product was deleted when member account was deleted! Error: %v", err)
	}

	if preserved.CreatedBy.Valid {
		t.Fatalf("expected created_by to be NULL via ON DELETE SET NULL, but it is valid: %v", preserved.CreatedBy)
	}
}

func TestFridgeInvitesAndLeaveFlowWithDB(t *testing.T) {
	ctx := context.Background()
	dbURL := "postgres://postgres:postgrespassword@localhost:5432/e_fridge?sslmode=disable"

	dbConn, err := database.Connect(ctx, dbURL)
	if err != nil {
		t.Skip("skipping DB integration test, cannot connect to PostgreSQL")
		return
	}
	defer dbConn.Close()

	queries := db.New(dbConn.Pool)
	service := NewService(queries, dbConn.Pool)

	unique := time.Now().UnixNano()
	ownerUser, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email:        fmt.Sprintf("inv_owner_%d@example.com", unique),
		Name:         "Invite Owner",
		PasswordHash: "dummyhash",
	})
	if err != nil {
		t.Fatalf("failed to create owner: %v", err)
	}

	guestUser, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email:        fmt.Sprintf("inv_guest_%d@example.com", unique),
		Name:         "Invite Guest",
		PasswordHash: "dummyhash",
	})
	if err != nil {
		t.Fatalf("failed to create guest: %v", err)
	}

	fridge, err := service.CreateFridge(ctx, ownerUser.ID, "Invite Test Fridge")
	if err != nil {
		t.Fatalf("failed to create fridge: %v", err)
	}

	// 1. Owner generates invite token
	invite, err := service.CreateInvite(ctx, fridge.ID, ownerUser.ID)
	if err != nil {
		t.Fatalf("failed to create invite: %v", err)
	}
	if invite.Token == "" {
		t.Fatalf("expected non-empty invite token")
	}

	// 2. Non-member inspects invite details
	details, err := service.GetInviteDetails(ctx, invite.Token)
	if err != nil {
		t.Fatalf("failed to get invite details: %v", err)
	}
	if details.FridgeName != "Invite Test Fridge" {
		t.Fatalf("expected fridge name 'Invite Test Fridge', got: %s", details.FridgeName)
	}

	// 3. Guest joins fridge using token
	joinedFridge, err := service.JoinFridge(ctx, invite.Token, guestUser.ID)
	if err != nil {
		t.Fatalf("failed to join fridge: %v", err)
	}
	if joinedFridge.ID != fridge.ID {
		t.Fatalf("expected joined fridge ID %s, got %s", fridge.ID, joinedFridge.ID)
	}

	// Verify guest is a member
	foundGuest := false
	for _, m := range joinedFridge.Members {
		if m.ID == guestUser.ID && m.Role == "member" {
			foundGuest = true
			break
		}
	}
	if !foundGuest {
		t.Fatalf("guest not found among fridge members with role 'member'")
	}

	// 4. Test Leave: Owner cannot leave while guest member exists
	err = service.LeaveFridge(ctx, fridge.ID, ownerUser.ID)
	if !errors.Is(err, ErrOwnerMustTransferOrDelete) {
		t.Fatalf("expected ErrOwnerMustTransferOrDelete, got: %v", err)
	}

	// 5. Transfer ownership to guest
	err = service.TransferOwnership(ctx, fridge.ID, ownerUser.ID, guestUser.ID)
	if err != nil {
		t.Fatalf("failed to transfer ownership: %v", err)
	}

	// Verify guest is now owner, former owner is admin
	updatedDetails, err := service.GetFridgeDetails(ctx, fridge.ID, guestUser.ID)
	if err != nil {
		t.Fatalf("failed to get updated fridge details: %v", err)
	}
	if updatedDetails.OwnerID != guestUser.ID {
		t.Fatalf("expected owner_id to be guestUser %s, got %s", guestUser.ID, updatedDetails.OwnerID)
	}

	// 6. Former owner leaves fridge
	err = service.LeaveFridge(ctx, fridge.ID, ownerUser.ID)
	if err != nil {
		t.Fatalf("expected former owner (now admin) to leave successfully: %v", err)
	}

	// 7. New owner leaves fridge when alone -> fridge is deleted
	err = service.LeaveFridge(ctx, fridge.ID, guestUser.ID)
	if err != nil {
		t.Fatalf("expected sole owner leaving to delete fridge: %v", err)
	}

	_, err = service.GetFridgeDetails(ctx, fridge.ID, guestUser.ID)
	if !errors.Is(err, ErrNotAuthorized) && !errors.Is(err, ErrFridgeNotFound) {
		t.Fatalf("expected fridge to be deleted after sole owner left, got err: %v", err)
	}
}

