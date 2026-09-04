package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/youdisn/lark-ob/internal/lark"
	"github.com/youdisn/lark-ob/internal/larkcli"
	"github.com/youdisn/lark-ob/internal/server"
	"github.com/youdisn/lark-ob/internal/store"
	"github.com/youdisn/lark-ob/internal/syncer"
)

func main() {
	if len(os.Args) < 2 {
		runStart(os.Args[1:])
		return
	}
	switch os.Args[1] {
	case "start":
		runStart(os.Args[2:])
	case "version":
		fmt.Println("lark-ob 0.1.0")
	default:
		fmt.Fprintf(os.Stderr, "用法: lark-ob start [--listen 127.0.0.1:8765]\n")
		os.Exit(2)
	}
}

func runStart(args []string) {
	loadEnvFile(".env")
	f := flag.NewFlagSet("start", flag.ExitOnError)
	listen := f.String("listen", "127.0.0.1:8765", "HTTP listen address")
	noOpen := f.Bool("no-open", false, "do not open browser")
	data := f.String("data", "", "SQLite database path")
	_ = f.Parse(args)
	cfg := lark.Config{AppID: os.Getenv("LARK_APP_ID"), AppSecret: os.Getenv("LARK_APP_SECRET"), APIBase: getenv("LARK_API_BASE", "https://open.feishu.cn"), AuthBase: getenv("LARK_AUTH_BASE", "https://accounts.feishu.cn"), RedirectURL: getenv("LARK_REDIRECT_URL", "http://127.0.0.1:8765/auth/lark/callback")}
	lc := lark.New(cfg)
	cliClient := larkcli.New()
	if *data == "" {
		*data = "data/lark-ob.db"
	}
	st, err := store.Open(*data)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	limit, _ := strconv.Atoi(getenv("LARK_SYNC_CHAT_LIMIT", "0"))
	sy := syncer.New(st, lc, cliClient, limit)
	poll, err := time.ParseDuration(getenv("LARK_POLL_INTERVAL", "8s"))
	if err != nil {
		poll = 8 * time.Second
	}
	if sy.HasToken(context.Background()) {
		go sy.Sync(context.Background())
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go sy.Run(ctx, poll)
	srv := &http.Server{Addr: *listen, Handler: server.New(st, lc, sy).Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = srv.Shutdown(shutdownCtx)
	}()
	url := "http://" + *listen
	log.Printf("Lark Inbox 已启动: %s (mode=%s)", url, sy.Status().Mode)
	if !*noOpen {
		go func() { time.Sleep(500 * time.Millisecond); openBrowser(url) }()
	}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
func getenv(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func loadEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" {
			if _, exists := os.LookupEnv(key); !exists {
				_ = os.Setenv(key, value)
			}
		}
	}
}
