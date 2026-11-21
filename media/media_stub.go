//go:build !darwin && !linux && !windows && !android

package media

import (
	"time"

	"go.hasen.dev/shirei"
)

// Player is a no-op placeholder on platforms without a media backend.
type Player struct{}

// Open reports ErrUnsupported on this platform.
func Open(path string) (*Player, error) { return nil, ErrUnsupported }

func (p *Player) Play()                    {}
func (p *Player) Pause()                   {}
func (p *Player) Seek(seconds float64)     {}
func (p *Player) SetLoop(loop bool)        {}
func (p *Player) SetMuted(muted bool)      {}
func (p *Player) Muted() bool              { return false }
func (p *Player) SetVolume(volume float32) {}
func (p *Player) Volume() float32          { return 0 }
func (p *Player) Playing() bool            { return false }
func (p *Player) Ended() bool              { return false }
func (p *Player) Position() time.Duration  { return 0 }
func (p *Player) Duration() time.Duration  { return 0 }
func (p *Player) Size() (int, int)         { return 0, 0 }
func (p *Player) ImageId() shirei.ImageId  { return 0 }
func (p *Player) Err() error               { return ErrUnsupported }
func (p *Player) View(maxSize shirei.Vec2) {}
func (p *Player) Close()                   {}
