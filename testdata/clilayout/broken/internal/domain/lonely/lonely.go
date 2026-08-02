package lonely

type Config struct{ Name string }

type Result struct{ Out string }

func Run(cfg Config) (Result, error) { return Result{Out: cfg.Name}, nil }
