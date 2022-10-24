package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/pelletier/go-toml/v2"
	"golang.org/x/sys/unix"
)

func main() {
	bin := flag.String("binary", os.Getenv("RUNTIME_BINARY"), "binary to install")
	name := flag.String("name", os.Getenv("RUNTIME_NAME"), "name of the runtime")
	hostPath := flag.String("host-dir", envOrDefault("HOST_DIR", "/bin"), "host path to install the runtime")
	criConfig := flag.String("cri-config", envOrDefault("CRI_CONFIG", "/etc/containerd/config.toml"), "path to the CRI config file")

	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, unix.SIGTERM)
	defer cancel()

	if err := install(ctx, *bin, *name, *hostPath, *criConfig); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	<-ctx.Done()
}

func install(ctx context.Context, bin, name, prefix, criConfig string) error {
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("error checking runtime binary: %w", err)
	}
	if err := os.MkdirAll(prefix, 0755); err != nil {
		return err
	}

	dir, err := os.MkdirTemp(prefix, ".c8d-runtime-install-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	tmpBin, err := os.CreateTemp(dir, name)
	if err != nil {
		return err
	}
	defer func() {
		tmpBin.Close()
		os.Remove(tmpBin.Name())
	}()

	src, err := os.Open(bin)
	if err != nil {
		return err
	}
	defer src.Close()

	if _, err := io.Copy(tmpBin, src); err != nil {
		return err
	}

	if err := os.Rename(tmpBin.Name(), prefix+"/"+filepath.Base(bin)); err != nil {
		return err
	}

	if err := os.Chmod(prefix+"/"+filepath.Base(bin), 0755); err != nil {
		return err
	}

	tmpCfg, err := os.CreateTemp(filepath.Dir(criConfig), "."+name+"-config-*")
	if err != nil {
		return err
	}
	defer func() {
		tmpCfg.Close()
		os.Remove(tmpCfg.Name())
	}()

	cfgf, err := os.Open(criConfig)
	if err != nil {
		return fmt.Errorf("could not open containerd config: %w", err)
	}
	defer cfgf.Close()

	typeName := strings.Replace(strings.Replace(filepath.Base(bin), "containerd-shim-", "io.containerd.", 1), "-", ".", -1)
	cfg, err := updateConfig(cfgf, name, typeName)
	if err != nil {
		return err
	}

	if _, err := tmpCfg.Write(cfg); err != nil {
		return err
	}

	if err := os.Rename(tmpCfg.Name(), criConfig); err != nil {
		return fmt.Errorf("error renaming temp config to real config: %w", err)
	}

	sd, err := dbus.NewSystemdConnectionContext(ctx)
	if err != nil {
		return err
	}

	defer sd.Close()

	ch := make(chan string, 1)
	if _, err := sd.RestartUnitContext(ctx, "containerd.service", "replace", ch); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		// TODO: reverse everything?
		return ctx.Err()
	case status := <-ch:
		if status != "done" {
			return fmt.Errorf("unexpected status: %s", status)
		}
	}

	return nil
}

func envOrDefault(env, def string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return def
}

func updateConfig(r io.Reader, name, runtimeType string) ([]byte, error) {
	var cfg map[string]interface{}

	if err := toml.NewDecoder(r).Decode(&cfg); err != nil {
		return nil, err
	}

	var pl map[string]interface{}
	if plI, ok := cfg["plugins"]; ok {
		pl = plI.(map[string]interface{})
	} else {
		pl = make(map[string]interface{})
	}

	var cri map[string]interface{}
	if criI, ok := pl["io.containerd.grpc.v1.cri"]; ok {
		cri = criI.(map[string]interface{})
	} else {
		cri = make(map[string]interface{})
	}

	var c8d map[string]interface{}
	if c8dI, ok := cri["containerd"]; ok {
		c8d = c8dI.(map[string]interface{})
	} else {
		c8d = make(map[string]interface{})
	}

	var runtimes map[string]interface{}
	if runtimesI, ok := c8d["runtimes"]; ok {
		runtimes = runtimesI.(map[string]interface{})
	} else {
		runtimes = make(map[string]interface{})
	}

	runtimes[name] = struct {
		RuntimeType string `toml:"runtime_type"`
	}{
		RuntimeType: runtimeType,
	}

	c8d["runtimes"] = runtimes
	cri["containerd"] = c8d
	pl["io.containerd.grpc.v1.cri"] = cri
	cfg["plugins"] = pl

	return toml.Marshal(cfg)
}
