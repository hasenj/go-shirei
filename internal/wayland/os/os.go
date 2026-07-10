// Copyright 2021 Neurlang project

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:

// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.

// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
// FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS
// IN THE SOFTWARE.

// Package os implements an operating system routines useful for graphics
package os

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"syscall"
)

// MkOsTemp is a Golang version of the popular C library function call
// The string can contain the pattern consisting of XXX that will be replaced
// with a high-entropy alphanumeric sequence, if you want more entropic string
// you can put more XXX (in multiples of 3 X) up to the recommended value of 27 X
// shorter sequence of XXX will make your MkOsTemp more prone to the failure
// the buffer tmpname will be overwritten by the high entropic buffer
// x1, x2, x3 are the three X characters we are replacing, it can be another
// character altogether.
func MkOsTemp(tmpdir string, tmpname []byte, flags int, x1 byte, x2 byte, x3 byte) (*os.File, error) {
	const alphabet = "123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

	var randbuf [5 * 9]byte
	var randbufI byte

	for i := 3; i <= len(tmpname); i++ {
		if (tmpname[i-3] == x1) && (tmpname[i-2] == x2) && (tmpname[i-1] == x3) {

			if randbufI == 0 {

				_, err := rand.Read(randbuf[:])
				if err != nil {
					return nil, fmt.Errorf("MkOsTemp: random read error: %w", err)
				}

				for o := 0; o < 9; o++ {
					n := (uint64(randbuf[5*o+0])) |
						(uint64(randbuf[5*o+1]) << 8) |
						(uint64(randbuf[5*o+2]) << 16) |
						(uint64(randbuf[5*o+3]) << 24) |
						(uint64(randbuf[5*o+4]) << 32)
					randbuf[3*o+0] = alphabet[n%61]
					n /= 61
					randbuf[3*o+1] = alphabet[n%61]
					n /= 61
					randbuf[3*o+2] = alphabet[n%61]
				}

			}

			for j := 0; j < 3; j++ {
				tmpname[i-3] = randbuf[randbufI]
				randbufI++
				i++
			}
			i--
			randbufI %= 27
		}
	}

	//println(string(tmpname))

	return os.CreateTemp(tmpdir, string(tmpname))
}

// CreateTmpfileCloexec creates a temp file that will be cloexec. In case of the ErrUnlink error, the fd is valid.
func CreateTmpfileCloexec(tmpdir, tmpname string) (*os.File, error) {

	var namebuf = []byte(tmpname)

	var fd, err = MkOsTemp(tmpdir, namebuf, syscall.O_CLOEXEC, 'X', 'X', 'X')
	if err != nil {
		return fd, fmt.Errorf("CreateTmpfileCloexec(%s): %w", namebuf, err)
	}
	if fd == nil {
		return fd, fmt.Errorf("CreateTmpfileCloexec: fd is nil")
	}

	if os.Remove(fd.Name()) != nil {
		return fd, ErrUnlink
	}

	return fd, nil
}

// ErrUnlink is a CreateTmpfileCloexec error that means a temp file
// was created but couldn't be deleted to make it anonymous.
var ErrUnlink = errors.New("CreateTmpfileCloexec: unlink error")

// ErrFileIsNil error means a temp file could not be created
var ErrFileIsNil = errors.New("CreateTmpfileCloexec: file is nil")

// CreateAnonymousFile creates anon fd, in case of the ErrUnlink error, the fd is valid.
// The file just isn't anonymous and can't be deleted. You can either ignore the ErrUnlink
// error and proceed, but it is your responsibility to Close the fd.
// In case of other errors, the fd is not valid and does not need to be closed.
func CreateAnonymousFile(size int64) (fd *os.File, err error) {

	const template = "go-lang-shared-XXXXXXXXXXXXXXXXXXXXXXXXXXX"

	path := os.Getenv("XDG_RUNTIME_DIR")

	path += string(os.PathSeparator)

	fd, err = CreateTmpfileCloexec(path, template)
	if err != nil && err != ErrUnlink {
		return fd, err
	}
	if fd == nil {
		return fd, ErrFileIsNil
	}
	for {
		err2 := fallocate(int(fd.Fd()), 0, 0, size)
		if err2 != nil {
			if err2 == syscall.EINTR {
				continue
			}
			_ = fd.Close()
			return nil, err2
		}
		break
	}

	return fd, err
}
