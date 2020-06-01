package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/xyproto/vt100"
)

// SetUpTerminateHandler sets up a signal handler for when ctrl-c is pressed
func (e *Editor) SetUpTerminateHandler(c *vt100.Canvas, status *StatusBar, tty *vt100.TTY) {
	sigChan := make(chan os.Signal, 1)

	for msg, sig := range map[string]os.Signal{
		"Abort":     syscall.SIGABRT,
		"Alarm":     syscall.SIGALRM,
		"Bus":       syscall.SIGBUS,
		"Child":     syscall.SIGCHLD,
		"CLD":       syscall.SIGCLD,
		"Continue":  syscall.SIGCONT,
		"FPE":       syscall.SIGFPE,
		"Hangup":    syscall.SIGHUP,
		"ILL":       syscall.SIGILL,
		"Interrupt": syscall.SIGINT,
		"IO":        syscall.SIGIO,
		"IOT":       syscall.SIGIOT,
		"Kill":      syscall.SIGKILL,
		"Pipe":      syscall.SIGPIPE,
		"POLL":      syscall.SIGPOLL,
		"PROF":      syscall.SIGPROF,
		"PWR":       syscall.SIGPWR,
		"QUIT":      syscall.SIGQUIT,
		"SEGV":      syscall.SIGSEGV,
		"STKFLT":    syscall.SIGSTKFLT,
		"STOP":      syscall.SIGSTOP,
		"SIGSYS":    syscall.SIGSYS,
		"ctrl-c":    syscall.SIGTERM,
		"TRAP":      syscall.SIGTRAP,
		"TSTP":      syscall.SIGTSTP,
		"TTIN":      syscall.SIGTTIN,
		"TTOU":      syscall.SIGTTOU,
		"UNUSED":    syscall.SIGUNUSED,
		"URG":       syscall.SIGURG,
		"USR1":      syscall.SIGUSR1,
		"USR2":      syscall.SIGUSR2,
		"VTALRM":    syscall.SIGVTALRM,
		"WINCH":     syscall.SIGWINCH,
		"XCPU":      syscall.SIGXCPU,
		"XFSZ":      syscall.SIGXFSZ,
	} {

		sig := sig
		msg := msg

		// Clear any previous terminate handlers
		//signal.Reset(sig)

		signal.Notify(sigChan, sig)
		go func() {
			for range sigChan {
				status.SetMessage(msg)
				status.Show(c, e)
			}
		}()

	}
}
