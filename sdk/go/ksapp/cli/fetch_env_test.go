package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// fakeFetcher 测试用 stub：返回固定 env / 固定 error。
type fakeFetcher struct {
	result map[string]string
	err    error
}

func (f *fakeFetcher) FetchEnv(ctx context.Context) (map[string]string, error) {
	return f.result, f.err
}

var sampleEnv = map[string]string{
	"DB_HOST":     "keystone-mysql",
	"DB_PORT":     "3306",
	"DB_USER":     "ksapp_writer",
	"DB_PASSWORD": "p@ssw0rd",
	"HMAC_SECRET": "hex32-deadbeef",
}

// ── dotenv 格式 ──────────────────────────────────────────────

func TestDoFetchEnv_Dotenv(t *testing.T) {
	var buf bytes.Buffer
	err := doFetchEnv(&buf, &fakeFetcher{result: sampleEnv}, "dotenv")
	if err != nil {
		t.Fatalf("doFetchEnv: %v", err)
	}
	out := buf.String()
	for _, marker := range []string{"BEGIN KEYSTONE MANAGED", "END KEYSTONE MANAGED"} {
		if !strings.Contains(out, marker) {
			t.Errorf("missing marker %q in:\n%s", marker, out)
		}
	}
	for k, v := range sampleEnv {
		if !strings.Contains(out, k) {
			t.Errorf("missing key %q", k)
		}
		if !strings.Contains(out, v) {
			t.Errorf("missing value %q", v)
		}
	}
}

func TestRenderDotenv_KeysSorted(t *testing.T) {
	var buf bytes.Buffer
	env := map[string]string{"ZEBRA": "z", "ALPHA": "a", "MIDDLE": "m"}
	renderDotenv(&buf, env, time.Unix(0, 0).UTC())

	var keys []string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "=") && !strings.Contains(line, "KEYSTONE MANAGED") {
			keys = append(keys, strings.SplitN(line, "=", 2)[0])
		}
	}
	want := []string{"ALPHA", "MIDDLE", "ZEBRA"}
	if fmt.Sprintf("%v", keys) != fmt.Sprintf("%v", want) {
		t.Errorf("keys order: %v, want %v", keys, want)
	}
}

func TestRenderDotenv_QuotesSpecialChars(t *testing.T) {
	var buf bytes.Buffer
	env := map[string]string{
		"PLAIN":      "abc123",
		"WITH_SPACE": "hello world",
		"WITH_HASH":  "abc#comment",
		"WITH_QUOTE": `val"x`,
	}
	renderDotenv(&buf, env, time.Unix(0, 0).UTC())
	out := buf.String()

	cases := map[string]string{
		"PLAIN=abc123":             "简单值不加引号",
		`WITH_SPACE="hello world"`: "空格触发加引号",
		`WITH_HASH="abc#comment"`:  "# 触发加引号",
		`WITH_QUOTE="val\"x"`:      "双引号转义",
	}
	for needle, why := range cases {
		if !strings.Contains(out, needle) {
			t.Errorf("expect %q (%s) in:\n%s", needle, why, out)
		}
	}
}

// ── json 格式 ────────────────────────────────────────────────

func TestDoFetchEnv_JSON(t *testing.T) {
	var buf bytes.Buffer
	err := doFetchEnv(&buf, &fakeFetcher{result: sampleEnv}, "json")
	if err != nil {
		t.Fatalf("doFetchEnv: %v", err)
	}
	var parsed map[string]string
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v (out=%s)", err, buf.String())
	}
	if len(parsed) != len(sampleEnv) {
		t.Errorf("parsed size: %d, want %d", len(parsed), len(sampleEnv))
	}
	for k, v := range sampleEnv {
		if parsed[k] != v {
			t.Errorf("parsed[%q]=%q, want %q", k, parsed[k], v)
		}
	}
}

// ── shell 格式 ────────────────────────────────────────────────

func TestDoFetchEnv_Shell(t *testing.T) {
	var buf bytes.Buffer
	env := map[string]string{"DB_HOST": "mysql", "DB_PASS": `p@$$"x"`}
	err := doFetchEnv(&buf, &fakeFetcher{result: env}, "shell")
	if err != nil {
		t.Fatalf("doFetchEnv: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `export DB_HOST="mysql"`) {
		t.Errorf("DB_HOST 未正确导出:\n%s", out)
	}
	// $ 与 " 都要转义为 \$ 和 \"
	if !strings.Contains(out, `export DB_PASS="p@\$\$\"x\""`) {
		t.Errorf("DB_PASS 转义错:\n%s", out)
	}
}

// ── 失败路径 ────────────────────────────────────────────────

func TestDoFetchEnv_FetchError(t *testing.T) {
	var buf bytes.Buffer
	err := doFetchEnv(&buf, &fakeFetcher{err: errors.New("boom")}, "dotenv")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "fetch keystone env failed") {
		t.Errorf("error message: %v", err)
	}
}

func TestDoFetchEnv_UnknownFormat(t *testing.T) {
	var buf bytes.Buffer
	err := doFetchEnv(&buf, &fakeFetcher{result: sampleEnv}, "xml")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unknown format") {
		t.Errorf("error message: %v", err)
	}
}
