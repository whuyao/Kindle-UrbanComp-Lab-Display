//go:build linux

package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"log"
	"syscall"
	"time"
	"unsafe"
)

type inputAxis struct{ Value, Min, Max, Fuzz, Flat, Resolution int32 }

func touchAxis(fd int, code uintptr) (inputAxis, error) {
	var axis inputAxis
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), 0x80184540+code, uintptr(unsafe.Pointer(&axis)))
	if err != 0 || axis.Max <= axis.Min {
		return axis, fmt.Errorf("touch axis %x: %v range=%d..%d", code, err, axis.Min, axis.Max)
	}
	return axis, nil
}

func startTouch(ctx context.Context, home func(), taps chan<- Tap) error {
	fd, err := syscall.Open("/dev/input/event1", syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	ax, ex := touchAxis(fd, 0x35)
	ay, ey := touchAxis(fd, 0x36)
	if ex != nil || ey != nil {
		ax, ex = touchAxis(fd, 0)
		ay, ey = touchAxis(fd, 1)
	}
	if ex != nil || ey != nil {
		syscall.Close(fd)
		return fmt.Errorf("touch coordinates unavailable: %v %v", ex, ey)
	}
	_, _, grabErr := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), 0x40044590, 1)
	if grabErr != 0 {
		syscall.Close(fd)
		return fmt.Errorf("touch grab: %v", grabErr)
	}
	log.Printf("TOUCH_READY x=%d..%d y=%d..%d", ax.Min, ax.Max, ay.Min, ay.Max)
	go func() {
		defer syscall.Close(fd) // Closing the descriptor releases the exclusive grab.
		recordSize := int(unsafe.Sizeof(syscall.Timeval{})) + 8
		buf := make([]byte, recordSize*32)
		var gesture homeGesture
		for {
			if ctx.Err() != nil {
				return
			}
			n, err := syscall.Read(fd, buf)
			if err == syscall.EAGAIN || err == syscall.EINTR {
				var readable syscall.FdSet
				bits := int(unsafe.Sizeof(readable.Bits[0])) * 8
				if fd >= len(readable.Bits)*bits {
					log.Print("TOUCH_FAILED: fd outside select range")
					home()
					return
				}
				readable.Bits[fd/bits] |= 1 << uint(fd%bits)
				// Input readiness wakes select immediately; the timeout is only
				// an idle cancellation fallback, not touch response latency.
				wait := syscall.Timeval{Sec: 5}
				_, selectErr := syscall.Select(fd+1, &readable, nil, nil, &wait)
				if selectErr != nil && selectErr != syscall.EINTR {
					log.Printf("TOUCH_FAILED: %v", selectErr)
					home()
					return
				}
				continue
			}
			if err != nil || n == 0 || n%recordSize != 0 {
				log.Printf("TOUCH_FAILED: %v bytes=%d; returning to Kindle", err, n)
				home()
				return
			}
			for at := 0; at < n; at += recordSize {
				payload := buf[at+recordSize-8 : at+recordSize]
				kind, code := binary.LittleEndian.Uint16(payload), binary.LittleEndian.Uint16(payload[2:])
				value := int32(binary.LittleEndian.Uint32(payload[4:]))
				if kind == 3 && (code == 0 || code == 0x35 || code == 1 || code == 0x36) {
					axis, size := ax, 758
					if code == 1 || code == 0x36 {
						axis, size = ay, 1024
					}
					v, ok := touchPixel(value, axis.Min, axis.Max, size)
					if !ok {
						gesture = homeGesture{}
						continue
					}
					value = v
				}
				if kind == 0 && code == 0 && gesture.releasing {
					log.Printf("TOUCH_RELEASE x=%d y=%d placed=%t multi=%t", gesture.x, gesture.y, gesture.placed, gesture.multi)
				}
				if gesture.event(kind, code, value, time.Now()) {
					tap := Tap{Start: gesture.start, End: image.Pt(gesture.x, gesture.y), At: time.Now()}
					if tap.in(homeTarget) {
						log.Print("HOME_BUTTON: returning to Kindle home")
						home()
						return
					}
					select {
					case taps <- tap:
					default:
					}
				}
			}
		}
	}()
	return nil
}
