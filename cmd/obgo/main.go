package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	"github.com/jookos/obgo-sync/internal/config"
	"github.com/jookos/obgo-sync/internal/couchdb"
	"github.com/jookos/obgo-sync/internal/crypto"
	"github.com/jookos/obgo-sync/internal/sync"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var envFile string

	root := &cobra.Command{
		Use:   "obgo",
		Short: "Sync an Obsidian vault with a CouchDB instance using the Livesync protocol",
	}

	root.PersistentFlags().StringVar(&envFile, "env-file", ".env", "path to .env file")

	root.AddCommand(pullCmd(&envFile))
	root.AddCommand(pushCmd(&envFile))
	root.AddCommand(listCmd(&envFile))

	return root
}

func pullCmd(envFile *string) *cobra.Command {
	var watch, watchLocal, watchRemote, silence, verbose, debug bool

	cmd := &cobra.Command{
		Use:   "pull [path]",
		Short: "Pull documents from CouchDB to local vault",
		Long: `Pull documents from CouchDB to the local vault.

Without a path argument every document is pulled. Provide a vault-relative path
to pull a single file, or a path ending with "/" to pull a folder and all its
contents.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filter := ""
			if len(args) == 1 {
				filter = args[0]
			}
			wl := watchLocal || watch
			wr := watchRemote || watch
			return run(cmd.Context(), *envFile, wl, wr, silence, verbose, debug, true, filter)
		},
	}
	cmd.Flags().BoolVarP(&watch, "watch", "w", false, "watch for both local and remote changes after pull")
	cmd.Flags().BoolVar(&watchLocal, "wl", false, "watch for local changes and push them after pull")
	cmd.Flags().BoolVar(&watchRemote, "wr", false, "watch for remote changes and pull them after pull")
	cmd.Flags().BoolVarP(&silence, "silence", "s", false, "suppress progress output")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "log each file synced during watch")
	cmd.Flags().BoolVarP(&debug, "debug", "d", false, "log every raw CouchDB change event (implies -v)")
	return cmd
}

func pushCmd(envFile *string) *cobra.Command {
	var watch, watchLocal, watchRemote, silence, verbose, debug bool

	cmd := &cobra.Command{
		Use:   "push [path]",
		Short: "Push local vault files to CouchDB",
		Long: `Push local vault files to CouchDB.

Without a path argument every local file is pushed. Provide a vault-relative
path to push a single file, or a path ending with "/" to push a folder and all
its contents.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filter := ""
			if len(args) == 1 {
				filter = args[0]
			}
			wl := watchLocal || watch
			wr := watchRemote || watch
			return run(cmd.Context(), *envFile, wl, wr, silence, verbose, debug, false, filter)
		},
	}
	cmd.Flags().BoolVarP(&watch, "watch", "w", false, "watch for both local and remote changes after push")
	cmd.Flags().BoolVar(&watchLocal, "wl", false, "watch for local changes and push them after push")
	cmd.Flags().BoolVar(&watchRemote, "wr", false, "watch for remote changes and pull them after push")
	cmd.Flags().BoolVarP(&silence, "silence", "s", false, "suppress progress output")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "log each file synced during watch")
	cmd.Flags().BoolVarP(&debug, "debug", "d", false, "log every raw CouchDB change event (implies -v)")
	return cmd
}

func listCmd(envFile *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list [path/]",
		Short: "List the contents of the remote vault",
		Long: `List documents stored in the remote CouchDB vault.

Without an argument all documents are listed. Provide a folder path ending with
"/" to list only that folder's contents.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prefix := ""
			if len(args) == 1 {
				prefix = args[0]
			}
			_ = godotenv.Load(*envFile)
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("configuration error: %w", err)
			}
			db, err := couchdb.New(cfg.CouchDBURL)
			if err != nil {
				return fmt.Errorf("CouchDB client error: %w", err)
			}
			cr := crypto.New(cfg.E2EEPassword)
			svc := sync.New(db, cr, cfg.DataPath)
			svc.SetObfuscationMode(cfg.PathObfuscationMode())
			docs, err := svc.List(cmd.Context(), prefix)
			if err != nil {
				return fmt.Errorf("list failed: %w", err)
			}
			for _, doc := range docs {
				t := time.UnixMilli(doc.MTime).Local()
				fmt.Printf("%-50s  %8s  %s\n", doc.Path, formatSize(doc.Size), t.Format("2006-01-02 15:04"))
			}
			return nil
		},
	}
}

func formatSize(bytes int64) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%d B", bytes)
	case bytes < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
	}
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func hostFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return u.Host
}

func run(parentCtx context.Context, envFile string, watchLocal, watchRemote, silence, verbose, debug, isPull bool, filter string) error {
	// Load .env file if present; ignore error if file is missing.
	_ = godotenv.Load(envFile)

	// Load configuration from environment.
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	// Create CouchDB client.
	db, err := couchdb.New(cfg.CouchDBURL)
	if err != nil {
		return fmt.Errorf("CouchDB client error: %w", err)
	}

	// Create crypto service.
	cr := crypto.New(cfg.E2EEPassword)

	// Create sync service.
	svc := sync.New(db, cr, cfg.DataPath)
	svc.SetObfuscationMode(cfg.PathObfuscationMode())

	// Set up context with cancellation for graceful shutdown.
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	// Handle OS signals for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	// Run the primary operation.
	host := hostFromURL(cfg.CouchDBURL)
	if isPull {
		if !silence {
			fmt.Fprintf(os.Stderr, "Pulling from %s...\n", host)
			var count int
			start := time.Now()
			svc.OnPullFile = func(n int) {
				count = n
				fmt.Fprintf(os.Stderr, "\rPulled %d file(s)", n)
			}
			svc.OnDeleteFile = func(path string) {
				fmt.Fprintf(os.Stderr, "\n  deleted %s", path)
			}
			if err := svc.Pull(ctx, filter); err != nil {
				fmt.Fprintln(os.Stderr)
				return fmt.Errorf("pull failed: %w", err)
			}
			fmt.Fprintf(os.Stderr, "\rPulled %d files in %s\n", count, formatDuration(time.Since(start)))
		} else {
			if err := svc.Pull(ctx, filter); err != nil {
				return fmt.Errorf("pull failed: %w", err)
			}
		}
	} else {
		if !silence {
			fmt.Fprintf(os.Stderr, "Pushing to %s...\n", host)
			var count int
			start := time.Now()
			svc.OnPushFile = func(n int) {
				count = n
				fmt.Fprintf(os.Stderr, "\rPushed %d file(s)", n)
			}
			svc.OnTombstone = func(path string) {
				fmt.Fprintf(os.Stderr, "\n  deleted %s (remote)", path)
			}
			if err := svc.Push(ctx, filter); err != nil {
				fmt.Fprintln(os.Stderr)
				return fmt.Errorf("push failed: %w", err)
			}
			fmt.Fprintf(os.Stderr, "\rPushed %d files in %s\n", count, formatDuration(time.Since(start)))
		} else {
			if err := svc.Push(ctx, filter); err != nil {
				return fmt.Errorf("push failed: %w", err)
			}
		}
	}

	// Start watch mode if requested (skip when a path filter is active).
	if (watchLocal || watchRemote) && filter == "" {
		if !silence {
			switch {
			case watchLocal && watchRemote:
				fmt.Fprintf(os.Stderr, "Watching for local and remote changes...\n")
			case watchLocal:
				fmt.Fprintf(os.Stderr, "Watching for local changes...\n")
			default:
				fmt.Fprintf(os.Stderr, "Watching for remote changes...\n")
			}
		}
		if verbose || debug {
			svc.OnWatchEvent = func(path string, toRemote bool) {
				if toRemote {
					fmt.Fprintf(os.Stderr, "  → %s (updated on local)\n", path)
				} else {
					fmt.Fprintf(os.Stderr, "  ← %s (updated on remote)\n", path)
				}
			}
			svc.OnDeleteFile = func(path string) {
				fmt.Fprintf(os.Stderr, "  ✗ %s (deleted on remote)\n", path)
			}
			svc.OnTombstone = func(path string) {
				fmt.Fprintf(os.Stderr, "  ✗ %s (deleted on local)\n", path)
			}
		}
		if debug {
			svc.OnRawChangeEvent = func(event couchdb.ChangeEvent) {
				b, _ := json.Marshal(event)
				fmt.Fprintf(os.Stderr, "  [raw] %s\n", b)
			}
		}
		if err := svc.Watch(ctx, watchLocal, watchRemote); err != nil {
			return fmt.Errorf("watch failed: %w", err)
		}
	}

	return nil
}
