package fridge

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

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
