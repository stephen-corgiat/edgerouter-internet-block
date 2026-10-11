package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

type IPSet interface {
	Add(ip string) error
	Del(ip string) error
	Members() ([]string, error)
}

type ExecIPSet struct {
	Name   string
	Binary string
}

func NewExecIPSet(name string) *ExecIPSet {
	return &ExecIPSet{Name: name, Binary: "ipset"}
}

func (s *ExecIPSet) run(args ...string) ([]byte, error) {
	cmd := exec.Command(s.Binary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("%s %s: %w: %s",
			s.Binary, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func (s *ExecIPSet) Add(ip string) error {
	_, err := s.run("add", s.Name, ip, "-exist")
	return err
}

func (s *ExecIPSet) Del(ip string) error {
	_, err := s.run("del", s.Name, ip, "-exist")
	return err
}

func (s *ExecIPSet) Members() ([]string, error) {
	out, err := s.run("list", s.Name)
	if err != nil {
		return nil, err
	}
	return parseMembers(out), nil
}

func parseMembers(out []byte) []string {
	members := []string{}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	inMembers := false
	for scanner.Scan() {
		line := scanner.Text()
		if !inMembers {
			if strings.HasPrefix(line, "Members:") {
				inMembers = true
			}
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Members may include comments after the IP; take first field.
		fields := strings.Fields(line)
		if len(fields) > 0 {
			members = append(members, fields[0])
		}
	}
	return members
}
