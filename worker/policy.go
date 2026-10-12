package worker

func Authorize(caller, method, path string) bool {
	return (path == "/readyz" || path == "/metrics") && (caller == "worker" || caller == "control")
}
