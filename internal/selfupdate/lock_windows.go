//go:build windows

package selfupdate

type updateLock struct{}

func acquireLock(string) (*updateLock, error) { return nil, ErrUnsupportedPlatform }
func (*updateLock) Release()                  {}
