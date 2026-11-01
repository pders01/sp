package templates

import (
	"syscall"

	"golang.org/x/sys/unix"
)

func waitTemplateExit(pid int) error {
	// Kqueue has no CLOEXEC creation flag on Darwin. Avoid leaking the
	// observer into another concurrently-started template process.
	syscall.ForkLock.RLock()
	kq, err := unix.Kqueue()
	if err == nil {
		unix.CloseOnExec(kq)
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return err
	}
	defer unix.Close(kq)
	changes := []unix.Kevent_t{{
		Ident: uint64(pid), Filter: unix.EVFILT_PROC,
		Flags: unix.EV_ADD | unix.EV_ENABLE | unix.EV_ONESHOT, Fflags: unix.NOTE_EXIT,
	}}
	events := make([]unix.Kevent_t, 1)
	for {
		n, err := unix.Kevent(kq, changes, events, nil)
		if err == unix.ESRCH {
			return nil // Our unreaped child already exited before registration.
		}
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		changes = nil
		if n == 0 {
			continue
		}
		if events[0].Flags&unix.EV_ERROR != 0 {
			if events[0].Data == int64(unix.ESRCH) {
				return nil
			}
			if events[0].Data != 0 {
				return unix.Errno(events[0].Data)
			}
			continue
		}
		return nil
	}
}
