// Command evalhistory measures how well a model picks up the person's earlier
// conversations - the recent-conversations list and the chat_history tool
// (docs/design/chat-history-plan.md, section 5) - with a real model, through a
// running server, so that a change to the prompt, the tool or the budgets can
// be compared before and after.
//
// Every trial is a person of its own: an account made for it in the server's
// database, given a history (planted straight into the database, with the
// dates a case needs), asked one question in a new conversation through the
// API like any client, and graded on what the answer says (grade.go). The
// account, its conversations and its session are deleted afterwards; the
// usage it caused stays in the usage log, marked with its address, because it
// was spent.
//
//	make ser   # in another terminal: the server this talks to
//	cd apps/server && go run ./cmd/evalhistory                  # every case, 3 times
//	go run ./cmd/evalhistory -cases recall,latest -runs 5 -v
//
// Real requests on real channels cost money: it asks with the cheapest model
// unless told otherwise (-model), and says what it spent.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	mrand "math/rand/v2"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"github.com/qunqin24/openchat/apps/server/internal/memory"
)

type options struct {
	server   string
	model    string
	runs     int
	parallel int
	cases    []evalCase
	seed     uint64
	timeout  time.Duration
	verbose  bool
	keep     bool
	out      string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "evalhistory:", err)
		os.Exit(1)
	}
}

func run() error {
	server := flag.String("server", "http://localhost:8080", "the running server's address")
	model := flag.String("model", "claude-haiku-5-5", "the model that answers; real requests cost money, keep it the cheapest")
	runs := flag.Int("runs", 3, "trials of each case")
	parallel := flag.Int("parallel", 3, "trials at a time")
	only := flag.String("cases", "", "comma-separated cases to run (default all): "+caseNames())
	seed := flag.Uint64("seed", 0, "seed for what the cases plant (default: from the clock); the same seed plants the same")
	timeout := flag.Duration("timeout", 3*time.Minute, "how long a trial's answer may take")
	verbose := flag.Bool("v", false, "print every answer and search, not only the failures'")
	keep := flag.Bool("keep", false, "keep the trials' accounts and conversations (for looking at them in the app)")
	out := flag.String("out", "", "also write every trial as JSON to this file")
	flag.Parse()

	o := options{server: strings.TrimRight(*server, "/"), model: *model, runs: *runs, parallel: max(1, *parallel),
		seed: *seed, timeout: *timeout, verbose: *verbose, keep: *keep, out: *out}
	if o.seed == 0 {
		o.seed = uint64(time.Now().UnixNano())
	}
	for _, c := range allCases {
		if *only == "" || slices.Contains(strings.Split(*only, ","), c.Name) {
			o.cases = append(o.cases, c)
		}
	}
	if len(o.cases) == 0 {
		return fmt.Errorf("no case named %q; the cases are %s", *only, caseNames())
	}

	// The same two candidates the server and adminctl load.
	for _, path := range []string{".env", "../../.env"} {
		_ = godotenv.Load(path)
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is not set (looked in the environment and .env)")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := ping(ctx, o.server); err != nil {
		return err
	}

	fmt.Printf("model %s, %d case(s) × %d, seed %d\n\n", o.model, len(o.cases), o.runs, o.seed)
	results := runAll(ctx, pool, o)
	report(os.Stdout, o, results)
	if o.out != "" {
		raw, err := json.MarshalIndent(map[string]any{"model": o.model, "seed": o.seed, "at": time.Now(), "trials": results}, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(o.out, raw, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func caseNames() string {
	names := make([]string, len(allCases))
	for i, c := range allCases {
		names[i] = c.Name
	}
	return strings.Join(names, ", ")
}

func ping(ctx context.Context, server string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server+"/healthz", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("no server at %s (start it with make ser): %w", server, err)
	}
	resp.Body.Close()
	return nil
}

// trialResult is one trial, as the report and -out give it.
type trialResult struct {
	Case     string   `json:"case"`
	Trial    int      `json:"trial"`
	Question string   `json:"question"`
	Answer   string   `json:"answer"`
	Status   string   `json:"status"`
	Searches []string `json:"searches"` // each chat_history call's arguments
	Pass     bool     `json:"pass"`
	Why      []string `json:"why,omitempty"`
	Seconds  float64  `json:"seconds"`
	Cost     float64  `json:"cost"`
	Tokens   [2]int64 `json:"tokens"` // input, output
	Err      string   `json:"error,omitempty"`
}

func runAll(ctx context.Context, pool *pgxpool.Pool, o options) []trialResult {
	type job struct {
		c     evalCase
		trial int
	}
	var jobs []job
	for _, c := range o.cases {
		for t := range o.runs {
			jobs = append(jobs, job{c, t})
		}
	}
	results := make([]trialResult, len(jobs))
	next := make(chan int)
	var wg sync.WaitGroup
	for range o.parallel {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				j := jobs[i]
				r := mrand.New(mrand.NewPCG(o.seed, uint64(i)))
				results[i] = trial(ctx, pool, o, j.c, j.trial, r)
				mark := "✓"
				if !results[i].Pass {
					mark = "✗"
				}
				fmt.Printf("%s %-9s #%d  %4.1fs  %s\n", mark, j.c.Name, j.trial+1, results[i].Seconds, strings.Join(results[i].Why, "; "))
			}
		}()
	}
	for i := range jobs {
		next <- i
	}
	close(next)
	wg.Wait()
	return results
}

// trial plants a history for a new person, asks, waits and grades.
func trial(ctx context.Context, pool *pgxpool.Pool, o options, c evalCase, n int, r *mrand.Rand) trialResult {
	now := time.Now()
	s := c.build(r, now)
	res := trialResult{Case: c.Name, Trial: n + 1, Question: s.Question}
	fail := func(err error) trialResult {
		res.Err = err.Error()
		res.Status = "error"
		res.Why = append(res.Why, err.Error())
		return res
	}

	user, token, email, err := newPerson(ctx, pool)
	if err != nil {
		return fail(fmt.Errorf("make the account: %w", err))
	}
	if !o.keep {
		defer forget(context.WithoutCancel(ctx), pool, user, email)
	}
	if err := plant(ctx, pool, user, append(fillers(now), s.Convs...)); err != nil {
		return fail(fmt.Errorf("plant the history: %w", err))
	}

	started := time.Now()
	runID, err := ask(ctx, o, token, s.Question)
	if err != nil {
		return fail(err)
	}
	status, code, err := wait(ctx, pool, runID, o.timeout)
	res.Seconds = time.Since(started).Seconds()
	if err != nil {
		return fail(err)
	}
	res.Status = status
	if code != "" {
		status += " (" + code + ")"
	}
	if err := readRun(ctx, pool, runID, &res); err != nil {
		return fail(err)
	}
	res.Pass, res.Why = grade(s.Expect, outcome{Status: status, Answer: res.Answer, Calls: len(res.Searches)})
	res.Status = status
	if o.verbose || !res.Pass {
		printTrial(res)
	}
	return res
}

// newPerson is an account for one trial, signed in with a session of its own.
// An administrator, so that whether the model is open to everyone and the
// quotas do not decide the outcome; it has no password, and its session lasts
// an hour.
func newPerson(ctx context.Context, pool *pgxpool.Pool) (uuid.UUID, string, string, error) {
	// Names are unique as well as addresses.
	name := "eval-history-" + uuid.NewString()[:8]
	email := name + "@test.invalid"
	var id uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, name, role) VALUES ($1, $2, 'admin') RETURNING id`, email, name).Scan(&id); err != nil {
		return uuid.Nil, "", "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return uuid.Nil, "", "", err
	}
	token := hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	if _, err := pool.Exec(ctx,
		`INSERT INTO sessions (user_id, token_hash, expires_at, user_agent) VALUES ($1, $2, now() + interval '1 hour', 'evalhistory')`,
		id, sum[:]); err != nil {
		return uuid.Nil, "", "", err
	}
	return id, token, email, nil
}

// forget deletes a trial's account and everything that is its (conversations,
// runs, session); the usage rows are kept and marked, as deleting an account
// in the admin panel does.
func forget(ctx context.Context, pool *pgxpool.Pool, user uuid.UUID, email string) {
	_ = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE usage_logs SET deleted_user = $2 WHERE user_id = $1`, user, email); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, user)
		return err
	})
}

// plant writes the conversations as if they had been had: at their times,
// their titles already checked (as a generated title is), each message a
// minute after the one before.
func plant(ctx context.Context, pool *pgxpool.Pool, user uuid.UUID, convs []plantedConv) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		for _, c := range convs {
			last := c.At.Add(time.Duration(len(c.Turns)-1) * time.Minute)
			var id uuid.UUID
			if err := tx.QueryRow(ctx,
				`INSERT INTO conversations (user_id, title, title_checked_hash, created_at, updated_at)
				 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
				user, c.Title, memory.Hash(c.Title), c.At, last).Scan(&id); err != nil {
				return err
			}
			for i, t := range c.Turns {
				if _, err := tx.Exec(ctx,
					`INSERT INTO messages (conversation_id, role, content, status, created_at, updated_at)
					 VALUES ($1, $2::message_role, $3, 'complete', $4, $4)`,
					id, t.Role, t.Content, c.At.Add(time.Duration(i)*time.Minute)); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// ask starts a conversation and sends the question, as a client does. The
// conversation is given a title, so no model is asked for one.
func ask(ctx context.Context, o options, token, question string) (uuid.UUID, error) {
	var conv struct {
		ID uuid.UUID `json:"id"`
	}
	if err := call(ctx, o, token, "/api/v1/conversations", map[string]any{"title": "Eval", "model": o.model}, &conv); err != nil {
		return uuid.Nil, fmt.Errorf("start the conversation: %w", err)
	}
	var sent struct {
		Run struct {
			ID uuid.UUID `json:"id"`
		} `json:"run"`
	}
	if err := call(ctx, o, token, "/api/v1/conversations/"+conv.ID.String()+"/messages",
		map[string]any{"content": question, "model": o.model}, &sent); err != nil {
		return uuid.Nil, fmt.Errorf("send the question: %w", err)
	}
	return sent.Run.ID, nil
}

func call(ctx context.Context, o options, token, path string, body, into any) error {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.server+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %d %s", path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return json.Unmarshal(data, into)
}

// wait is until the run is no longer running: how it ended, and its error
// code when it failed.
func wait(ctx context.Context, pool *pgxpool.Pool, run uuid.UUID, timeout time.Duration) (string, string, error) {
	deadline := time.Now().Add(timeout)
	for {
		var status, code string
		if err := pool.QueryRow(ctx, `SELECT status::text, COALESCE(error_code, '') FROM runs WHERE id = $1`, run).Scan(&status, &code); err != nil {
			return "", "", err
		}
		if status != "running" {
			return status, code, nil
		}
		if time.Now().After(deadline) {
			return "", "", fmt.Errorf("no answer after %s", timeout)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// readRun takes the final answer, the chat_history calls and the usage of a
// run.
func readRun(ctx context.Context, pool *pgxpool.Pool, run uuid.UUID, res *trialResult) error {
	rows, err := pool.Query(ctx,
		`SELECT content, COALESCE(parts, '[]'::jsonb) FROM messages
		 WHERE run_id = $1 AND role = 'assistant' ORDER BY created_at, id`, run)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var content string
		var parts []byte
		if err := rows.Scan(&content, &parts); err != nil {
			return err
		}
		if strings.TrimSpace(content) != "" {
			res.Answer = content
		}
		var calls []struct {
			Type string          `json:"type"`
			Name string          `json:"name"`
			Args json.RawMessage `json:"arguments"`
		}
		_ = json.Unmarshal(parts, &calls)
		for _, c := range calls {
			if c.Type == "tool_call" && c.Name == "chat_history" {
				res.Searches = append(res.Searches, string(c.Args))
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return pool.QueryRow(ctx,
		`SELECT COALESCE(sum(cost), 0)::float8, COALESCE(sum(input_tokens), 0)::bigint, COALESCE(sum(output_tokens), 0)::bigint
		 FROM usage_logs WHERE run_id = $1`, run).Scan(&res.Cost, &res.Tokens[0], &res.Tokens[1])
}

var printMu sync.Mutex

func printTrial(r trialResult) {
	printMu.Lock()
	defer printMu.Unlock()
	fmt.Printf("    ┌ %s #%d: %s\n", r.Case, r.Trial, r.Question)
	for _, s := range r.Searches {
		fmt.Printf("    │ chat_history %s\n", s)
	}
	for _, line := range strings.Split(strings.TrimSpace(r.Answer), "\n") {
		fmt.Printf("    │ %s\n", line)
	}
	fmt.Println("    └")
}

func report(w io.Writer, o options, results []trialResult) {
	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "case\tpassed\tused tool\tcalls/trial\tseconds\tcost\twhat it measures")
	var passed, total int
	var cost float64
	for _, c := range o.cases {
		var p, used, calls, n int
		var secs, spent float64
		for _, r := range results {
			if r.Case != c.Name {
				continue
			}
			n++
			if r.Pass {
				p++
			}
			if len(r.Searches) > 0 {
				used++
			}
			calls += len(r.Searches)
			secs += r.Seconds
			spent += r.Cost
		}
		passed, total, cost = passed+p, total+n, cost+spent
		fmt.Fprintf(tw, "%s\t%d/%d\t%d/%d\t%.1f\t%.1f\t%.4f\t%s\n", c.Name, p, n, used, n,
			float64(calls)/float64(max(n, 1)), secs/float64(max(n, 1)), spent, c.About)
	}
	fmt.Fprintf(tw, "all\t%d/%d\t\t\t\t%.4f\t\n", passed, total, cost)
	tw.Flush()
}
