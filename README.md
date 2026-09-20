# E-Fridge API

Backend service for the E-Fridge smart food and nutrition management platform, built with Go and PostgreSQL. Provides high-performance RESTful endpoints for pantry tracking, AI-driven recipe and meal plan generation, calorie and macro accounting, and multi-user spaces.

## Table of Contents

- Overview
- Tech Stack
- Key Modules and Features
- Security and Rate Limiting
- Prerequisites
- Configuration
- Getting Started
- Database Migrations
- API Endpoints Reference
- Testing
- Project Structure

## Overview

E-Fridge API serves as the core business logic and data persistence engine for the E-Fridge platform. It integrates with LLM providers (via OpenRouter) to deliver context-aware recipe creation based on available ingredients in the user's fridge, tracks nutritional goals, and automates shopping lists.

## Tech Stack

- Language: Go 1.22+
- HTTP Router: Chi v5 (`github.com/go-chi/chi/v5`)
- Database: PostgreSQL 16
- Database Driver: pgx v5 (`github.com/jackc/pgx/v5`)
- Query Generator: sqlc (type-safe SQL)
- Migrations: golang-migrate
- Authentication: JWT (`golang-jwt/jwt/v5`), bcrypt, Google OAuth verification
- External Integrations:
  - OpenRouter API (Meta Llama 3.3 70B, DeepSeek Chat) for AI Chef and Meal Planner
  - Resend API / SMTP for two-step email verification
  - OpenFoodFacts for barcode scanning and nutrition data lookup

## Key Modules and Features

- Authentication and Accounts:
  - Two-step email verification with 6-digit codes.
  - Automatic cleanup of unverified registrations after 48 hours.
  - Google Sign-In with automatic account provisioning.
  - Profile update and complete account deletion with cascading cleanup.
- Fridge Spaces:
  - Multi-fridge support per user account.
  - Real-time inventory tracking with expiration dates, categories, and stock quantities.
- AI Chef:
  - Interactive chat assistant with context of products currently available in the user's fridge.
  - Automatic structured recipe extraction (ingredients, steps, preparation time, macros).
  - Ingredient availability matching and one-click missing item shopping suggestions.
- Meal Planner:
  - Single-day meal plan generation (breakfast, lunch, dinner) with pantry verification.
  - Rate-limited per-meal regeneration.
  - Meal completion toggling and detailed recipe view modal support.
- Nutrition Tracker:
  - Daily calorie, protein, fat, and carbohydrate targets.
  - Meal logging with direct consumption from fridge stock.
  - Log deletion with automatic product quantity restoration back to the fridge.
- Shopping List:
  - Batch addition of missing recipe items.
  - Item categorization and purchased-status toggling.

## Security and Rate Limiting

- Token Bucket Rate Limiter:
  - Protects sensitive endpoints (login, register, verification code dispatch) against brute-force attacks.
  - Enforces request throttling on AI generation routes (AI Chef and Meal Planner) to prevent quota exhaustion and spam.
- Password Requirements: Minimum 8-character passwords with bcrypt hashing.
- CORS Configuration: Configurable allowed origins with credentials support.
- Data Sanitation: Strict database foreign keys and cascading rules.

## Prerequisites

- Go 1.22 or higher
- Docker and Docker Compose (or local PostgreSQL 16 instance)
- sqlc (optional, for regenerating database queries)

## Configuration

Copy the sample environment file and adjust variables:

```bash
cp .env.example .env
```

Key environment parameters:

| Variable | Description | Default |
| --- | --- | --- |
| `PORT` | HTTP server listening port | `8080` |
| `DATABASE_URL` | PostgreSQL connection string | `postgres://user:pass@localhost:5432/e_fridge?sslmode=disable` |
| `JWT_SECRET` | Secret key for signing JWT tokens | Random string (min 32 chars in production) |
| `JWT_ACCESS_MINUTES` | Access token lifespan | `15` |
| `JWT_REFRESH_DAYS` | Refresh token lifespan | `30` |
| `CORS_ALLOWED_ORIGINS` | Comma-separated list of allowed origins | `http://localhost:5173,http://localhost:3000` |
| `APP_ENV` | Application environment (`development` or `production`) | `development` |
| `OPENROUTER_API_KEY` | OpenRouter API token for LLM completions | Required for AI features |
| `OPENROUTER_FREE_MODEL` | Primary AI model | `meta-llama/llama-3.3-70b-instruct:free` |
| `OPENROUTER_FALLBACK_MODEL` | Fallback AI model | `deepseek/deepseek-chat` |
| `EMAIL_PROVIDER` | Email provider (`resend` or `smtp`) | `resend` |
| `RESEND_API_KEY` | Resend API token | Required if using Resend |
| `GOOGLE_CLIENT_ID` | Google OAuth Client ID for identity verification | Required for Google Auth |

## Getting Started

1. Start PostgreSQL:
   ```bash
   docker run --name e_fridge_postgres -e POSTGRES_PASSWORD=postgrespassword -e POSTGRES_DB=e_fridge -p 5432:5432 -d postgres:16-alpine
   ```

2. Run the application:
   ```bash
   go run cmd/api/main.go
   ```
   Database migrations execute automatically on server startup.

3. Verify health endpoint:
   ```bash
   curl http://localhost:8080/health
   ```

## Database Migrations

Migrations are located in `internal/database/migrations` (embedded in the Go binary) and run automatically on application start.

To manually manage migrations using `golang-migrate`:

```bash
# Apply migrations
migrate -path migrations -database "$DATABASE_URL" up

# Rollback one step
migrate -path migrations -database "$DATABASE_URL" down 1
```

## API Endpoints Reference

Base URL: `/api/v1`

### Authentication (`/auth`)
- `POST /auth/register` - Step 1: Submit details and dispatch email verification code
- `POST /auth/verify-email` - Step 2: Confirm 6-digit code and create user account
- `POST /auth/login` - Authenticate via email/password
- `POST /auth/google` - Authenticate via Google ID token
- `POST /auth/refresh` - Refresh access token using refresh cookie
- `POST /auth/logout` - Invalidate session and clear auth cookies
- `GET /auth/profile` - Fetch current user profile
- `PUT /auth/profile` - Update user name
- `DELETE /auth/profile` - Permanently delete account and all associated user data

### Fridges (`/fridges`)
- `GET /fridges` - List all fridges accessible to user
- `POST /fridges` - Create a new fridge space
- `GET /fridges/{id}` - Get fridge details and members
- `POST /fridges/{id}/members` - Invite member by email

### Products (`/products`)
- `GET /products` - List products in current fridge
- `POST /products` - Add product manually
- `PUT /products/{id}` - Update product details
- `DELETE /products/{id}` - Remove product from fridge
- `GET /products/barcode/{barcode}` - Lookup product info via OpenFoodFacts
- `POST /products/estimate` - Estimate nutritional values using AI

### Nutrition (`/nutrition`)
- `GET /nutrition/goals` - Get user daily macro and calorie targets
- `PUT /nutrition/goals` - Update daily nutritional targets
- `GET /nutrition/summary` - Get daily aggregated calorie/macro consumption
- `GET /nutrition/logs` - List meal consumption logs for a date
- `POST /nutrition/eat` - Log product consumption and deduct stock from fridge
- `PUT /nutrition/logs/{id}` - Update logged meal quantity/macros
- `DELETE /nutrition/logs/{id}` - Delete log and restore product quantity to fridge

### AI Chef (`/chef`)
- `GET /chef/history` - Fetch recent conversation messages
- `POST /chef/chat` - Send query to AI Chef with fridge inventory context
- `DELETE /chef/history` - Clear conversation history

### Meal Planner (`/planner`)
- `GET /planner` - Fetch meal plan for a specific date
- `POST /planner/generate-day` - Generate full day meal plan (breakfast, lunch, dinner)
- `POST /planner/generate-meal` - Generate single meal slot
- `POST /planner` - Manually create planned meal
- `PUT /planner/{id}` - Update meal details
- `DELETE /planner/{id}` - Remove planned meal
- `PATCH /planner/{id}/toggle` - Mark meal as completed or uncompleted

### Shopping List (`/shopping`)
- `GET /shopping` - Get current shopping list items
- `POST /shopping` - Add item to shopping list
- `POST /shopping/batch` - Batch add items (e.g. missing recipe ingredients)
- `PUT /shopping/{id}` - Update item quantity or name
- `DELETE /shopping/{id}` - Remove item
- `PATCH /shopping/{id}/toggle` - Toggle purchased state

### Saved Recipes (`/recipes`)
- `GET /recipes` - List user saved recipes
- `POST /recipes` - Save recipe
- `GET /recipes/{id}` - Get recipe details
- `DELETE /recipes/{id}` - Delete recipe from collection

## Testing

Run unit and integration tests across all internal packages:

```bash
go test ./...
```

Run tests with coverage analysis:

```bash
go test -coverprofile=coverage.txt ./...
go tool cover -func=coverage.txt
```

## Project Structure

```text
E-Fridge-Api/
├── cmd/
│   └── api/
│       └── main.go                 # Application entry point & router setup
├── internal/
│   ├── config/                     # Environment and app configuration
│   ├── database/                   # DB connection pool and migration runner
│   ├── db/                         # Generated sqlc database models and queries
│   ├── middleware/                 # Rate limiting, auth JWT, and CORS middleware
│   ├── modules/
│   │   ├── auth/                   # Authentication, registration, and profile
│   │   ├── chef/                   # AI Chef conversation and recipe logic
│   │   ├── cooking/                # Product deduction logic
│   │   ├── fridge/                 # Fridge space management
│   │   ├── nutrition/              # Daily goals, logging, and stock restoration
│   │   ├── planner/                # Meal plan generation and slots
│   │   ├── products/               # Fridge inventory and barcode service
│   │   ├── recipes/                # Saved recipes collection
│   │   └── shopping/               # Shopping list management
│   └── pkg/
│       ├── crypto/                 # Password hashing utilities
│       ├── jwt/                    # Token signing and validation
│       └── response/               # Standardized JSON response formatting
├── migrations/                     # Raw SQL migration files
├── queries/                        # sqlc query definition files
├── sqlc.yaml                       # sqlc configuration
├── go.mod
└── go.sum
```