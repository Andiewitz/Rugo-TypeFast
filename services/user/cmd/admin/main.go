package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"typefast/services/user/internal/audit"
	"typefast/services/user/internal/identity"
	"typefast/services/user/internal/revocation"
	"typefast/services/user/internal/revocation/bus"
	"typefast/services/user/internal/storage/postgres"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "admin:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return fmt.Errorf("a command is required")
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://kit:kit_dev_password@localhost:5432/kit?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	store, err := postgres.New(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer store.Close()

	// The CLI is a separate process, so a status change here has to be broadcast
	// by hand or other instances wait for their next refresh.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	publisher, closeBus, err := busPublisher(logger)
	if err != nil {
		return err
	}
	defer closeBus()

	switch args[0] {
	case "list-users":
		return listUsers(ctx, store, args[1:])
	case "list-roles":
		return listRoles(ctx, store)
	case "set-status":
		return setStatus(ctx, store, publisher, logger, args[1:])
	case "grant-role":
		return grantRole(ctx, store, args[1:])
	case "revoke-role":
		return revokeRole(ctx, store, args[1:])
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: admin <command> [arguments]

commands:
  list-users [-query text] [-limit 50]
  list-roles
  set-status <user-id> <active|suspended>
  grant-role <user-id> <user|admin>
  revoke-role <user-id> <user|admin>`)
}

func listUsers(ctx context.Context, store *postgres.Store, args []string) error {
	flags := flag.NewFlagSet("list-users", flag.ContinueOnError)
	query := flags.String("query", "", "search email or display name")
	limit := flags.Int("limit", 50, "maximum results")
	if err := flags.Parse(args); err != nil {
		return err
	}

	users, total, err := store.ListUsers(ctx, *query, *limit, 0)
	if err != nil {
		return err
	}
	output := make([]userOutput, 0, len(users))
	for _, user := range users {
		output = append(output, outputUser(user))
	}
	return printJSON(map[string]any{"users": output, "total": total})
}

func listRoles(ctx context.Context, store *postgres.Store) error {
	roles, err := store.ListRoles(ctx)
	if err != nil {
		return err
	}
	return printJSON(map[string]any{"roles": roles})
}

func setStatus(ctx context.Context, store *postgres.Store, publisher revocation.Publisher, logger *slog.Logger, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("set-status requires <user-id> <active|suspended>")
	}
	id, err := uuid.Parse(args[0])
	if err != nil {
		return fmt.Errorf("invalid user id: %w", err)
	}
	status := identity.Status(args[1])
	if status != identity.StatusActive && status != identity.StatusSuspended {
		return fmt.Errorf("status must be active or suspended")
	}

	user, err := store.SetUserStatus(ctx, id, status)
	if err != nil {
		return err
	}
	_ = store.Record(ctx, audit.Event{
		SubjectID: &id,
		Action:    "admin.cli_status_changed",
		Metadata:  map[string]any{"status": string(status)},
	})

	// Same marker the API computes, so replicas merge this with the same rules.
	if publisher != nil {
		registry := revocation.NewRegistry(store, logger)
		registry.SetPublisher(publisher)
		before := markerFor(user)
		if status == identity.StatusSuspended {
			if before.IsZero() {
				before = time.Now().UTC()
			}
			registry.Revoke(id, before)
		} else {
			registry.Restore(id, before)
		}
		logger.Info("revocation broadcast", "user_id", id, "status", string(status))
	}

	return printJSON(outputUser(user))
}

func grantRole(ctx context.Context, store *postgres.Store, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("grant-role requires <user-id> <user|admin>")
	}
	id, role, err := parseRoleArgs(args)
	if err != nil {
		return err
	}
	if err := store.GrantRole(ctx, id, role); err != nil {
		return err
	}
	_ = store.Record(ctx, audit.Event{
		SubjectID: &id,
		Action:    "admin.cli_role_granted",
		Metadata:  map[string]any{"role": role},
	})
	fmt.Printf("granted role %s to %s\n", role, id)
	return nil
}

func revokeRole(ctx context.Context, store *postgres.Store, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("revoke-role requires <user-id> <user|admin>")
	}
	id, role, err := parseRoleArgs(args)
	if err != nil {
		return err
	}
	if err := store.RevokeRole(ctx, id, role); err != nil {
		return err
	}
	_ = store.Record(ctx, audit.Event{
		SubjectID: &id,
		Action:    "admin.cli_role_revoked",
		Metadata:  map[string]any{"role": role},
	})
	fmt.Printf("revoked role %s from %s\n", role, id)
	return nil
}

// markerFor returns the stored cut-off for a user, or the zero time when they
// never had one.
func markerFor(user identity.User) time.Time {
	if user.RevokedBefore == nil {
		return time.Time{}
	}
	return *user.RevokedBefore
}

// busPublisher opens the revocation bus when it is configured. It returns a nil
// publisher when the bus is off, which keeps the CLI silent on a single host.
func busPublisher(logger *slog.Logger) (revocation.Publisher, func(), error) {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("REVOCATION_BUS")))
	switch mode {
	case "", "none":
		return nil, func() {}, nil
	case "redis":
	default:
		return nil, func() {}, fmt.Errorf("REVOCATION_BUS must be none or redis, got %q", mode)
	}

	url := strings.TrimSpace(os.Getenv("REDIS_URL"))
	if url == "" {
		return nil, func() {}, fmt.Errorf("REDIS_URL is required when REVOCATION_BUS=redis")
	}
	channel := strings.TrimSpace(os.Getenv("REVOCATION_CHANNEL"))
	if channel == "" {
		channel = "kit:revocations"
	}

	client, err := bus.NewRedis(url, channel, logger)
	if err != nil {
		return nil, func() {}, err
	}
	return client, func() { _ = client.Close() }, nil
}

func parseRoleArgs(args []string) (uuid.UUID, string, error) {
	id, err := uuid.Parse(args[0])
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("invalid user id: %w", err)
	}
	role := args[1]
	if role != "user" && role != "admin" {
		return uuid.Nil, "", fmt.Errorf("role must be user or admin")
	}
	return id, role, nil
}

type userOutput struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
}

func outputUser(user identity.User) userOutput {
	return userOutput{
		ID:          user.ID.String(),
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Status:      string(user.Status),
		CreatedAt:   user.CreatedAt.Format(time.RFC3339),
	}
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
