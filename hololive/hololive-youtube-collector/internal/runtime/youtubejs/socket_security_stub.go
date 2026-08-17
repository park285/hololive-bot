//go:build !linux

package youtubejs

func requireHelperPlatform() error {
	return ErrUnsupportedHelperPlatform
}

func verifyHelperSocket(string) error {
	return ErrUnsupportedHelperPlatform
}
