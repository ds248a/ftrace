# ftrace - трассировка системных вызовов

Для работы должен поддерживаться механизм динасической трассировки 'kprobes'.

Возврящаются данные в формате
```go
type Event struct {
	PID int         // идентификатор процесса
	Name string     // наименование события
	IsSyscall bool  // true для 'syscall event'
	Args map[string]string
}
```

Пример использования


```go
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ds248a/ftrace"
)

func main() {
	subEvents := []string{
		"sched/sched_process_fork",
		"sched/sched_process_exec",
		"sched/sched_process_exit",
	}

	watcher := ftrace.NewProbe("test_probe", "sys_execve", subEvents)

	if err := watcher.Reset(); err != nil && watcher.Enabled() {
		fmt.Printf("ftrace.Reset() error: %v\n", err)
		return
	}

	if err := watcher.Enable(); err != nil {
		fmt.Printf("%s\n", err)
		return
	}

	setupSignals(func() {
		if err := watcher.Disable(); err != nil {
			fmt.Printf("%s\n", err)
		} else {
			fmt.Printf("Probe disabled.\n")
		}
	})

	for e := range watcher.Events() {
		if e.IsSyscall == true {
			fmt.Printf("SYSCALL %s\n", e)

		} else if _, ok := e.Args["filename"]; ok && e.Name == "sched_process_exec" {
			fmt.Printf("        %s\n", e)

		} else if e.Name == "sched_process_exit" {
			fmt.Printf("        %s\n", e)
		}
	}
}

func setupSignals(cb func()) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	go func() {
		_ = <-sigChan
		cb()
		os.Exit(0)
	}()
}

```