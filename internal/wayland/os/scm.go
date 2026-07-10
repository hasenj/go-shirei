//go:build !(aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris)
// +build !aix,!darwin,!dragonfly,!freebsd,!linux,!netbsd,!openbsd,!solaris

package os

import "errors"

// SocketControlMessage is a socket control message
type SocketControlMessage struct{}

// Sockaddr is a socket address
type Sockaddr struct{}

var ErrUnsupportedOS = errors.New("unsupported os")

// ParseSocketControlMessage calls a system call to parse a Socket Control Message
func ParseSocketControlMessage([]byte) (scms []SocketControlMessage, err error) {
	return nil, ErrUnsupportedOS
}

// ParseUnixRights calls a system call to parse unix rights
func ParseUnixRights(*SocketControlMessage) (fds []int, err error) {
	return nil, ErrUnsupportedOS
}

func fallocate(fd int, mode uint32, off int64, size int64) error {
	return ErrUnsupportedOS
}

// UnixRights calls a system call
func UnixRights(int) []byte {
	return nil
}

// Sendmsg sends information on fd using a Sendmsg system call
func Sendmsg(fd int, msg []byte, oob []byte, sockaddr Sockaddr, z int) error {
	return ErrUnsupportedOS
}

// Mmap calls the system call to map memory on a fd
func Mmap(fd int, offset int64, length int, prot int, flags int) (data []byte, err error) {
	return nil, ErrUnsupportedOS
}

// Munmap calls the system call to unmap memory
func Munmap(data []byte) error {
	return ErrUnsupportedOS
}

// Close closes the fd
func Close(fd int) error {
	return ErrUnsupportedOS
}

// ProtRead Pages may be read
const ProtRead = 0x1

// ProtWrite Pages may be written
const ProtWrite = 0x2

// MapShared Share this mapping
const MapShared = 0x01

// MapPrivate Private mapping
const MapPrivate = 0x02
