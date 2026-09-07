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

	"github.com/youdisn/lark-ob/internal/agent"
	"github.com/youdisn/lark-ob/internal/impression"
	"github.com/youdisn/lark-ob/internal/initializer"
	"github.com/youdisn/lark-ob/internal/knowledge"
	"github.com/youdisn/lark-ob/internal/knowledge/larksource"
	"github.com/youdisn/lark-ob/internal/lark"
	"github.com/youdisn/lark-ob/internal/larkcli"
	"github.com/youdisn/lark-ob/internal/maintenance"
	"github.com/youdisn/lark-ob/internal/media"
	"github.com/youdisn/lark-ob/internal/memory"
	"github.com/youdisn/lark-ob/internal/profile"
	"github.com/youdisn/lark-ob/internal/server"
	"github.com/youdisn/lark-ob/internal/store"
	"github.com/youdisn/lark-ob/internal/syncer"
)

var version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		runStart(os.Args[1:])
		return
	}
	switch os.Args[1] {
	case "start":
		runStart(os.Args[2:])
	case "version":
		fmt.Println("lark-ob " + version)
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
	profileService := profile.New(st, cliClient)
	sy.SetProfileQueue(profileService)
	var knowledgeService *knowledge.Service
	if runner, runnerErr := larksource.NewCommandRunner(); runnerErr != nil {
		log.Printf("知识库模块未启用: %v", runnerErr)
	} else {
		knowledgeService = knowledge.NewService(st, larksource.New(runner))
	}
	decayHalfLifeDays, decayErr := strconv.ParseFloat(getenv("MEMORY_DECAY_HALF_LIFE_DAYS", "90"), 64)
	if decayErr != nil || decayHalfLifeDays <= 0 {
		decayHalfLifeDays = 90
	}
	memoryService := memory.NewServiceWithConfig(st, memory.Config{DecayHalfLifeDays: decayHalfLifeDays})
	memoryTimeout, timeoutErr := time.ParseDuration(getenv("AGENT_MEMORY_TIMEOUT", "5m"))
	if timeoutErr != nil {
		memoryTimeout = 5 * time.Minute
	}
	replyTimeout, replyTimeoutErr := time.ParseDuration(getenv("AGENT_REPLY_TIMEOUT", "1m"))
	if replyTimeoutErr != nil {
		replyTimeout = time.Minute
	}
	agentEngine := agent.New(context.Background(), agent.Config{
		APIKey:               os.Getenv("AGENT_API_KEY"),
		BaseURL:              getenv("AGENT_BASE_URL", "http://127.0.0.1:18766/v1"),
		Model:                getenv("AGENT_MODEL", "grok-4.5"),
		ReasoningEffort:      getenv("AGENT_MEMORY_REASONING_EFFORT", "medium"),
		ReplyReasoningEffort: getenv("AGENT_REPLY_REASONING_EFFORT", "low"),
		MemoryTimeout:        memoryTimeout,
		ReplyTimeout:         replyTimeout,
	}, st, memoryService)
	sy.SetChatContextQueue(agentEngine)
	lookbackDays, lookbackErr := strconv.Atoi(getenv("MEMORY_LOOKBACK_DAYS", "30"))
	if lookbackErr != nil || lookbackDays <= 0 {
		lookbackDays = 30
	}
	memoryWorkers, workersErr := strconv.Atoi(getenv("MEMORY_INITIALIZATION_WORKERS", "2"))
	if workersErr != nil || memoryWorkers <= 0 {
		memoryWorkers = 2
	}
	initService := initializer.New(initializer.Config{LookbackDays: lookbackDays, Workers: memoryWorkers}, st, sy, agentEngine)
	impressionService := impression.New(st, agentEngine, memoryWorkers)
	dailyHour, dailyMinute := parseClock(getenv("PROFILE_DAILY_TIME", "03:00"), 3, 0)
	dailyLocation, locationErr := time.LoadLocation(getenv("PROFILE_TIMEZONE", "Asia/Shanghai"))
	if locationErr != nil {
		log.Printf("画像任务时区无效，回退 UTC: %v", locationErr)
		dailyLocation = time.UTC
	}
	maintenanceService := maintenance.New(maintenance.Config{LookbackDays: lookbackDays, Workers: memoryWorkers,
		Hour: dailyHour, Minute: dailyMinute, Location: dailyLocation}, st, sy, agentEngine, impressionService)
	mediaService := media.New(st, cliClient, media.DefaultCacheDir())
	poll, err := time.ParseDuration(getenv("LARK_POLL_INTERVAL", "8s"))
	if err != nil {
		poll = 8 * time.Second
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go profileService.Run(ctx, 2)
	agentEngine.RunChatContextWorkers(ctx, memoryWorkers)
	go func() {
		if sy.HasToken(ctx) {
			// Existing installations also need a per-chat context immediately;
			// position/history backfills continue independently afterwards.
			if chats, err := st.Chats(ctx); err == nil {
				for _, chat := range chats {
					agentEngine.EnqueueChatContext(chat.ID)
				}
			}
			if err := sy.BackfillCLIPositions(ctx, time.Now().AddDate(0, 0, -lookbackDays)); err != nil && ctx.Err() == nil {
				log.Printf("最近消息顺序回填未完成: %v", err)
			}
			if err := initService.Run(ctx); err != nil && ctx.Err() == nil {
				log.Printf("首次记忆初始化未完成: %v", err)
			}
			if chats, err := st.Chats(ctx); err == nil {
				for _, chat := range chats {
					agentEngine.EnqueueChatContext(chat.ID)
				}
			}
		}
		maintenanceService.Run(ctx)
	}()
	go sy.Run(ctx, poll)
	srv := &http.Server{Addr: *listen, Handler: server.New(st, lc, sy, knowledgeService, agentEngine, memoryService, initService, profileService, impressionService, maintenanceService, mediaService).Handler(), ReadHeaderTimeout: 5 * time.Second}
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

func parseClock(value string, fallbackHour, fallbackMinute int) (int, int) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return fallbackHour, fallbackMinute
	}
	hour, hourErr := strconv.Atoi(parts[0])
	minute, minuteErr := strconv.Atoi(parts[1])
	if hourErr != nil || minuteErr != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return fallbackHour, fallbackMinute
	}
	return hour, minute
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
