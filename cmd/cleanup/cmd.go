package cleanup

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/inngest/inngest/pkg/db/postgres/retention"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/urfave/cli/v3"
)

const dryRunPrefix = "dry-run:"

func Command() *cli.Command {
	return &cli.Command{
		Name:  "cleanup",
		Usage: "Delete finished run history from the self-hosted Postgres database",
		Description: "Deletes runs that ended more than --retention ago, along with their " +
			"spans, traces, history, and unreferenced events. Runs that have not ended are never " +
			"deleted. --retention takes a duration such as 30d or 720h. Prefix it with dry-run: " +
			"to only report what would be deleted, e.g. --retention=dry-run:7d. " +
			"See docs/POSTGRES_RETENTION.md.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "postgres-uri",
				Usage:    "PostgreSQL database URI to clean",
				Required: true,
				Sources:  cli.EnvVars("INNGEST_POSTGRES_URI"),
			},
			&cli.StringFlag{
				Name:  "retention",
				Usage: "Keep finished history for this long, e.g. 30d or 720h. Prefix with dry-run: to report without deleting",
				Value: "30d",
			},
			&cli.IntFlag{
				Name:  "batch-size",
				Usage: "Maximum rows removed by each DELETE statement",
				Value: 10000,
			},
		},
		Action: run,
	}
}

func run(ctx context.Context, cmd *cli.Command) error {
	window, dryRun, err := parseRetention(cmd.String("retention"))
	if err != nil {
		return err
	}

	db, err := sql.Open("pgx", cmd.String("postgres-uri"))
	if err != nil {
		return fmt.Errorf("opening postgres: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connecting to postgres: %w", err)
	}

	results, err := retention.Run(ctx, db, retention.Config{
		Retention: window,
		BatchSize: int(cmd.Int("batch-size")),
		DryRun:    dryRun,
	})
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

// parseRetention reads the --retention value: an optional dry-run: prefix
// followed by a duration. Without the prefix, the command deletes.
func parseRetention(v string) (time.Duration, bool, error) {
	dryRun := false
	if rest, ok := strings.CutPrefix(v, dryRunPrefix); ok {
		dryRun = true
		v = rest
	}

	if days, ok := strings.CutSuffix(v, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, false, fmt.Errorf("invalid --retention %q: %w", v, err)
		}
		return time.Duration(n) * 24 * time.Hour, dryRun, nil
	}

	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, false, fmt.Errorf("invalid --retention %q: %w", v, err)
	}
	return d, dryRun, nil
}
