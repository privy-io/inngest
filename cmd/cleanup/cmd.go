package cleanup

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/inngest/inngest/pkg/db/postgres/retention"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/urfave/cli/v3"
)

func Command() *cli.Command {
	return &cli.Command{
		Name:  "cleanup",
		Usage: "Delete finished run history from the self-hosted Postgres database",
		Description: "Deletes runs that ended more than --retention-days ago, along with their " +
			"spans, traces, history, and unreferenced events. Runs that have not ended are never " +
			"deleted. Dry run is the default; pass --dry-run=false to delete. " +
			"See docs/POSTGRES_RETENTION.md.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "postgres-uri",
				Usage:    "PostgreSQL database URI to clean",
				Required: true,
				Sources:  cli.EnvVars("INNGEST_POSTGRES_URI"),
			},
			&cli.IntFlag{
				Name:  "retention-days",
				Usage: "Keep finished history for this many days",
				Value: 30,
			},
			&cli.IntFlag{
				Name:  "batch-size",
				Usage: "Maximum rows removed by each DELETE statement",
				Value: 10000,
			},
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "Report what would be deleted without deleting anything",
				Value: true,
			},
		},
		Action: run,
	}
}

func run(ctx context.Context, cmd *cli.Command) error {
	db, err := sql.Open("pgx", cmd.String("postgres-uri"))
	if err != nil {
		return fmt.Errorf("opening postgres: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connecting to postgres: %w", err)
	}

	dryRun := cmd.Bool("dry-run")
	cfg := retention.Config{
		Retention: time.Duration(cmd.Int("retention-days")) * 24 * time.Hour,
		BatchSize: int(cmd.Int("batch-size")),
		DryRun:    dryRun,
	}

	results, err := retention.Run(ctx, db, cfg)
	if err != nil {
		return err
	}

	verb := "deleted"
	if dryRun {
		verb = "would delete"
	}
	for _, r := range results {
		fmt.Printf("%-18s %s %d\n", r.Table, verb, r.Rows)
	}
	return nil
}
