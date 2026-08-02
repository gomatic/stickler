package create

type Config struct{ Name string }

type Result struct{ ID string }

func Run(cfg Config) (Result, error) { return Result{ID: cfg.Name}, nil }
