package greet

type Config struct{ Name string }

type Result struct{ Message string }

func Run(cfg Config) (Result, error) { return Result{Message: cfg.Name}, nil }
