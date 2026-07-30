//go:build !windows

package hooks

type APMTracker struct{}

func RegisterLowLevelKeyboardHook(handler func()) (uintptr, error) { return 0, nil }
func RegisterLowLevelMouseHook(handler func()) (uintptr, error)    { return 0, nil }
func UnhookWindowsHookEx(hook uintptr) error                       { return nil }
func GetForegroundWindowTitle() (string, error)                    { return "", nil }
func StartAPMTracker() *APMTracker                                 { return &APMTracker{} }
func (t *APMTracker) CurrentAPM() int                              { return 0 }
func (t *APMTracker) Stop()                                        {}
func StartMessageLoop()                                            {}
