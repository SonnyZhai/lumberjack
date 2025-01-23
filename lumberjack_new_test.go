package lumberjack

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestDailyNamingAndCleanup 测试自定义的 "app_YYYY-MM-DD.log" 命名 + cleanup 效果
func TestDailyNamingAndCleanup(t *testing.T) {
	// 1) 创建临时目录
	dir := t.TempDir()

	// 2) 主文件 "app.log"
	logFile := filepath.Join(dir, "app.log")

	// 我们将用这个变量来模拟当前时间
	var fakeNow int64
	// 先设置成 2023-01-02 00:00:00
	start := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	// 在测试中把 currentTime 覆盖
	currentTime = func() time.Time {
		// 把 fakeNow 当做 UnixNano 存储
		return time.Unix(0, atomic.LoadInt64(&fakeNow)).UTC()
	}
	defer func() {
		// 恢复默认
		currentTime = time.Now
	}()
	// 初始化 fakeNow
	atomic.StoreInt64(&fakeNow, start.UnixNano())

	// 3) 创建 logger
	//    假设你已经在 backupName / prefixAndExt / timeFromName 改了逻辑
	//    这里使用 MaxAge=1, MaxBackups=3, Compress=true 做演示
	l := &Logger{
		Filename:   logFile,
		MaxSize:    10, // 设置小一点，方便测试
		MaxAge:     1,  // 保留1天
		MaxBackups: 3,  // 最多3个旧文件
		Compress:   true,
		LocalTime:  true, // 如你需要本地时区
	}

	// 4) 写入一些日志
	_, err := l.Write([]byte("Hello day1\n"))
	if err != nil {
		t.Fatalf("write day1 failed: %v", err)
	}

	// 5) 第一次 rotate - 理论上立即把app.log改名成 "app_2023-01-01.log" (因为 now=1/2 0:00, -24h=1/1)
	if err := l.Rotate(); err != nil {
		t.Fatalf("rotate day1 failed: %v", err)
	}

	// 检查目录里应该有:
	//   - "app_2023-01-01.log" (旧文件)
	//   - "app.log" (新文件, size=0)
	checkFiles(t, dir, []string{
		"app_2023-01-01.log",
		"app.log",
	}, []string{})

	// 6) 写一点新日志
	_, err = l.Write([]byte("Hello day2\n"))
	if err != nil {
		t.Fatalf("write day2 failed: %v", err)
	}

	// 7) 模拟过了一天 -> 来到 2023-01-03 00:00，再 rotate
	next := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)
	atomic.StoreInt64(&fakeNow, next.UnixNano())

	if err := l.Rotate(); err != nil {
		t.Fatalf("rotate day2 failed: %v", err)
	}

	// 此时:
	//   app.log => rename => "app_2023-01-02.log"  (因为 now=1/3, -24h=1/2)
	//   lumberjack 会检查 oldLogFiles(),
	//   因为 MaxAge=1 => 距离1/1已经过去2天, "app_2023-01-01.log" 应该被删除 or 压缩?
	//   具体看 timeFromName() 的解析 + modTime 是否超过1天.

	// 但此处因为只过去1天(1/2到1/3), "app_2023-01-01.log" 距1/3是2天前 => MaxAge=1 => 需要被删除/或先压缩然后删除?
	// Actually, "app_2023-01-01.log" might be older than 1 day from now=1/3 => ~48h difference
	// So it should be removed (or compressed first, but end result is removed).
	// "app_2023-01-02.log" (刚 rename 出来的), "app.log" (新空文件).

	checkFiles(t, dir, []string{
		"app_2023-01-02.log", // day2
		"app.log",            // new
	}, []string{
		// "app_2023-01-01.log" should be removed or possibly "app_2023-01-01.log.gz" if we compress
		// but if it's older than 1 day from now(1/3), it should end up removed.
		// If you had config to keep them 2 days, you'd see it compressed
	})

	// 8) 再写一次日志
	_, err = l.Write([]byte("Hello day3\n"))
	if err != nil {
		t.Fatalf("write day3 failed: %v", err)
	}

	// 9) 模拟再过一天 -> 2023-01-04 => rotate
	next = time.Date(2023, 1, 4, 0, 0, 0, 0, time.UTC)
	atomic.StoreInt64(&fakeNow, next.UnixNano())
	if err := l.Rotate(); err != nil {
		t.Fatalf("rotate day3 failed: %v", err)
	}

	// 结果:
	//   - 旧的 "app.log" => "app_2023-01-03.log"
	//   - lumberjack 会清理 or 压缩 "app_2023-01-02.log" if it's too old?
	//   - 具体看 MaxAge=1 & TimeDiff.

	// ... 你可以进一步 assert 目录文件, 判断 MaxBackups=3 行为, etc.
	// 这里只做一个演示, 具体逻辑要看你timeFromName()如何计算旧文件时长.

	// done
}

func TestWriteAndCheckContent(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "app.log")

	// 创建 Logger
	l := &Logger{
		Filename:  logFile,
		MaxSize:   50, // 50MB
		LocalTime: true,
	}

	msg1 := "Hello, Lumberjack!\n"
	msg2 := "This is a test line.\n"

	// 写入 msg1
	_, err := l.Write([]byte(msg1))
	if err != nil {
		t.Fatalf("write msg1 failed: %v", err)
	}

	// 读取文件内容，检查是否包含 msg1
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read file %s error: %v", logFile, err)
	}
	if string(content) != msg1 {
		t.Fatalf("file content mismatch\n got: %q\nwant: %q", string(content), msg1)
	}

	// 再写入 msg2
	_, err = l.Write([]byte(msg2))
	if err != nil {
		t.Fatalf("write msg2 failed: %v", err)
	}

	// 再次读取文件，检查是否包含两段
	content, err = os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read file %s error: %v", logFile, err)
	}
	expected := msg1 + msg2
	if string(content) != expected {
		t.Fatalf("file content mismatch\n got: %q\nwant: %q", string(content), expected)
	}
}

func TestMultipleRotateSameDay(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "app.log")

	// Mock time
	baseTime := time.Date(2023, 1, 2, 10, 0, 0, 0, time.UTC)
	var fakeNow int64
	atomic.StoreInt64(&fakeNow, baseTime.UnixNano())
	currentTime = func() time.Time {
		return time.Unix(0, atomic.LoadInt64(&fakeNow)).UTC()
	}
	defer func() { currentTime = time.Now }()

	// Logger: MaxSize 小一点，测试写满立即 rotate
	l := &Logger{
		Filename:  logFile,
		MaxSize:   1, // 1MB
		LocalTime: true,
		// 其他配置省略
	}

	// 写入 0.8MB
	big := make([]byte, 800*1024) // 800KB
	if _, err := l.Write(big); err != nil {
		t.Fatalf("initial write failed: %v", err)
	}

	// 超过 1MB 前再写 300KB
	more := make([]byte, 300*1024)
	if _, err := l.Write(more); err != nil {
		t.Fatalf("second write for rotate: %v", err)
	}
	// lumberjack 会自动 rotate => rename => app_YYYY-MM-DD.log (the same day 2023-01-02)

	// 检查目录中的文件
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir error: %v", err)
	}
	// 可能你需要断言“app_2023-01-01.log” or “app_2023-01-02.log” 有无冲突
	// 但如果 backupName 里是 now.Add(-24h)，那这里 now=1/2 => yesterday=1/1 => 可能是 app_2023-01-01.log
	// 关键看你实际实现
	t.Logf("files after second write => %v", listNames(files))

	// 继续写 => 再度 rotate
	// 同一天(1/2)第三次 rotate, 可能覆盖或生成同名
	if err := l.Rotate(); err != nil {
		t.Fatalf("manual rotate same day: %v", err)
	}

	files2, err := ioutil.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir error: %v", err)
	}
	t.Logf("files after manual rotate => %v", listNames(files2))

	// 看你的自定义 backupName 是否在同一天多次 rotate 会覆盖同一个 app_YYYY-MM-DD.log？
	// 如果是，就需做额外断言
}

// listNames is a helper to show file names in logs
func listNames(infos []os.FileInfo) []string {
	var r []string
	for _, f := range infos {
		r = append(r, f.Name())
	}
	return r
}

func TestMaxBackup(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "app.log")

	// Mock time
	baseTime := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	var fakeNow int64
	atomic.StoreInt64(&fakeNow, baseTime.UnixNano())
	currentTime = func() time.Time {
		return time.Unix(0, atomic.LoadInt64(&fakeNow)).UTC()
	}
	defer func() { currentTime = time.Now }()

	l := &Logger{
		Filename:   logFile,
		MaxBackups: 3,
		// 不管 MaxAge, Compress, ...
		LocalTime: true,
	}

	// 写一次 => rotate => 生成 #1
	if _, err := l.Write([]byte("data #1\n")); err != nil {
		t.Fatalf("write #1: %v", err)
	}
	if err := l.Rotate(); err != nil {
		t.Fatalf("rotate #1: %v", err)
	}

	// 写一次 => rotate => #2
	atomic.StoreInt64(&fakeNow, baseTime.Add(24*time.Hour).UnixNano()) // next day
	if _, err := l.Write([]byte("data #2\n")); err != nil {
		t.Fatalf("write #2: %v", err)
	}
	if err := l.Rotate(); err != nil {
		t.Fatalf("rotate #2: %v", err)
	}

	// 写一次 => rotate => #3
	atomic.StoreInt64(&fakeNow, baseTime.Add(48*time.Hour).UnixNano()) // day +2
	if _, err := l.Write([]byte("data #3\n")); err != nil {
		t.Fatalf("write #3: %v", err)
	}
	if err := l.Rotate(); err != nil {
		t.Fatalf("rotate #3: %v", err)
	}

	// 再写 => rotate => #4
	atomic.StoreInt64(&fakeNow, baseTime.Add(72*time.Hour).UnixNano()) // day +3
	if _, err := l.Write([]byte("data #4\n")); err != nil {
		t.Fatalf("write #4: %v", err)
	}
	if err := l.Rotate(); err != nil {
		t.Fatalf("rotate #4: %v", err)
	}

	// 现在理应有 #1, #2, #3, #4共4个旧文件, 但 MaxBackups=3 => cleanup() 应删除最老的一份 (#1).
	// 目录里只保留 #2, #3, #4 + "app.log"

	files, err := ioutil.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir error: %v", err)
	}
	var names []string
	for _, f := range files {
		names = append(names, f.Name())
	}
	t.Logf("final files: %v", names)

	// 断言: #1 不存在 (最老那份)
	// #2, #3, #4 存在, 以及 app.log
	// 具体文件名可能是 "app_2022-12-31.log", "app_2023-01-01.log", etc.
	// 要根据你 backupName() 逻辑 & time shift. 自行 assert.
}

func TestCompressBeforeExpire(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "app.log")

	var fakeNow int64
	baseTime := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	atomic.StoreInt64(&fakeNow, baseTime.UnixNano())
	currentTime = func() time.Time {
		return time.Unix(0, atomic.LoadInt64(&fakeNow)).UTC()
	}
	defer func() { currentTime = time.Now }()

	// 设置 Compress=true，但 MaxAge=7(七天)
	l := &Logger{
		Filename:   logFile,
		MaxSize:    1, //很小,容易写满
		Compress:   true,
		MaxAge:     7, // 不会被删除
		MaxBackups: 0, // 不限制备份数量
		LocalTime:  true,
	}

	// 写 1.5MB => 触发自动 rotate, 生成 "app_2023-01-01.log" (yesterday=1/1)
	big1 := make([]byte, 800*1024) // 0.8MB
	if _, err := l.Write(big1); err != nil {
		t.Fatalf("write1: %v", err)
	}

	// 这里又写 0.7MB => size 累加到1.5MB，总体超过1MB => 触发rotate
	big2 := make([]byte, 700*1024)
	if _, err := l.Write(big2); err != nil {
		t.Fatalf("write2: %v", err)
	}

	// rotate 后, 旧文件并不大于 MaxAge => lumberjack 会把它压缩成 .gz
	// 但注意, lumberjack 的默认实现: only if we have more logs appended or manual rotate again
	//  Trigger a second rotate forcibly to run cleanup
	if err := l.Rotate(); err != nil {
		t.Fatalf("manual rotate: %v", err)
	}

	// 现在 check 目录，看是否出现 .gz
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	var foundGZ bool
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".gz") {
			foundGZ = true
			break
		}
	}
	if !foundGZ {
		t.Fatal("expected at least one .gz file, but none found")
	}
}

// checkFiles 帮助函数: 检查目录里包含的文件 (exist) 以及不包含的文件 (notExist).
func checkFiles(t *testing.T, dir string, exist, notExist []string) {
	t.Helper()
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir error: %v", err)
	}
	var names []string
	for _, f := range files {
		names = append(names, f.Name())
	}
	for _, e := range exist {
		if !contains(names, e) {
			t.Fatalf("expected file %q not found in directory. got: %v", e, names)
		}
	}
	for _, ne := range notExist {
		if contains(names, ne) {
			t.Fatalf("file %q should NOT exist, but found in %v", ne, names)
		}
	}
}

func contains(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
}
