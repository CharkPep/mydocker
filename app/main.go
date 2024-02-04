package main

import (
	"fmt"
	ImageManager2 "mydocker/app/ImageManager"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func resolveAbsPath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}

	return filepath.Abs(path)
}

func CreateTemporaryRoot() (root string, err error) {
	if root, err = os.MkdirTemp("", "my_docker"); err != nil {
		return "", err
	}
	//if entrypoint, err = resolveAbsPath(entrypoint); err != nil {
	//	return "", err
	//}
	//if err := os.MkdirAll(root+filepath.Dir(entrypoint), 0755); err != nil {
	//	return "", err
	//}
	//if err := os.Link(entrypoint, root+entrypoint); err != nil {
	//	return "", err
	//}

	return root, nil
}

func CopyIntoRoot(root, entrypoint string) error {
	var err error
	if entrypoint, err = resolveAbsPath(entrypoint); err != nil {
		return err
	}
	if err := os.MkdirAll(root+filepath.Dir(entrypoint), 0755); err != nil {
		return err
	}
	if err := os.Link(entrypoint, root+entrypoint); err != nil {
		return err
	}

	return nil
}

func IsolateRoot(path string) error {
	if err := syscall.Chroot(path); err != nil {
		return err
	}
	if err := syscall.Unshare(syscall.CLONE_NEWPID); err != nil {
		return err
	}
	return os.Chdir("/")
}

func main() {
	container := strings.Split(os.Args[2], ":")
	command := os.Args[3]
	args := os.Args[4:len(os.Args)]

	tokenAuthenticator := ImageManager2.OCIAuthenticator{}
	imageManager := ImageManager2.NewImageManager(&tokenAuthenticator)
	root, err := CreateTemporaryRoot()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	err = imageManager.Pull(container[0], container[1], root)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	err = CopyIntoRoot(root, command)
	err = IsolateRoot(root)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer func(path string) {
		os.Remove(path)
	}(root)
	fmt.Printf("Running %s %v\n", command, args)
	cmd := exec.Command(command, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	os.Exit(0)
}
