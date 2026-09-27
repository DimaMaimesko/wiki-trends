package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/dmytromaimesko/wiki-trends/internal/wikiapi"
)

func cmdCache(args []string) error {
	fs := flag.NewFlagSet("cache", flag.ContinueOnError)
	dir := fs.String("cache-dir", "", "override the cache directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	sub := "stats"
	if fs.NArg() > 0 {
		sub = fs.Arg(0)
	}
	cache, err := wikiapi.OpenCache(*dir)
	if err != nil {
		return err
	}
	switch sub {
	case "stats":
		n, b := cache.Stat()
		fmt.Printf("cache dir:  %s\nresponses:  %d\nsize:       %.1f MB\n", cache.Dir, n, float64(b)/(1<<20))
		fmt.Println("\nPageview counts for finalised days never change, so they are cached permanently.")
		fmt.Println("Only windows touching the last 45 days are re-checked (6h TTL).")
	case "clear":
		if err := cache.Clear(); err != nil {
			return err
		}
		fmt.Println("cache cleared:", cache.Dir)
	default:
		return fmt.Errorf("unknown cache subcommand %q (want stats or clear)", sub)
	}
	return nil
}

// cmdDoctor verifies the things that actually break in this environment: network
// reachability, the User-Agent header Wikimedia requires, and a writable cache.
// It is the first thing to run when a fetch fails for an unclear reason.
func cmdDoctor(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	dir := fs.String("cache-dir", "", "override the cache directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	fmt.Println("wikitrends", Version)

	cache, err := wikiapi.OpenCache(*dir)
	if err != nil {
		return fmt.Errorf("cache unusable: %w", err)
	}
	n, b := cache.Stat()
	fmt.Printf("cache:        %s (%d responses, %.1f MB) OK\n", cache.Dir, n, float64(b)/(1<<20))

	cl := wikiapi.New(cache)
	defer cl.Close()
	fmt.Printf("user-agent:   %s\n", cl.UserAgent)

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	end := time.Now().UTC().AddDate(0, 0, -5)
	start := end.AddDate(0, 0, -3)
	if _, err := cl.ProjectViews(ctx, "en.wikipedia", "all-access", "user", start, end); err != nil {
		fmt.Fprintf(os.Stderr, "pageviews:    FAILED: %v\n", err)
		return fmt.Errorf("pageviews API unreachable")
	}
	fmt.Println("pageviews:    reachable OK")

	if _, err := cl.Entity(ctx, "Q1666254", []string{"en", "cs"}); err != nil {
		fmt.Fprintf(os.Stderr, "wikidata:     FAILED: %v\n", err)
		return fmt.Errorf("wikidata API unreachable")
	}
	fmt.Println("wikidata:     reachable OK")

	fmt.Printf("\nlatest window a run will use: up to %s (Wikimedia finalises with a ~2 day lag)\n",
		time.Now().UTC().AddDate(0, 0, -2).Format("2006-01-02"))
	return nil
}
