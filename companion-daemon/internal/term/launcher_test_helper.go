package term

func launcherWrapper(_ interface{}) ManagedPTYLauncherV1 {
	return NewPTYLauncher()
}
