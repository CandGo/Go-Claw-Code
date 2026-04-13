package commands

// SetReflectionEnabled toggles the reflection pattern on the runtime.
func (a *RuntimeControlAdapter) SetReflectionEnabled(enabled bool) {
	if a.rt == nil {
		return
	}
	a.rt.SetReflectionEnabled(enabled)
}
