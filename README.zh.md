# go-webrtcvad

[![Go Tests](https://github.com/baabaaox/go-webrtcvad/actions/workflows/go-tests.yml/badge.svg)](https://github.com/baabaaox/go-webrtcvad/actions/workflows/go-tests.yml)

本项目基于最新 WebRTC LKGR (2025-02-07) 的 VAD，实现为 Go 包，源代码来自 [WebRTC lkgr 提交 8e55dca89f4e39241f9e3ecd25ab0ebbf5d1ab37](https://webrtc.googlesource.com/src/+/8e55dca89f4e39241f9e3ecd25ab0ebbf5d1ab37)，并重写自 [maxhawkins/go-webrtcvad](https://github.com/maxhawkins/go-webrtcvad)。

## 线程安全与性能改进

- 每个 `VadInst` 现在持有独立的 wasm 上下文锁，所有 wasm 调用都会串行化，支持多 goroutine 并发使用同一个实例。
- 内部维护一个 wasm 上下文池。`Create` 会复用已初始化的实例，避免重复编译/实例化，从而降低延迟和内存占用。
- `Free` 会把上下文归还池中，`ValidRateAndFrameLength` 等辅助函数也会透明地借用/归还上下文，保持原有 API 不变。
- 推荐在开发阶段运行 `go test -race ./...` 检查潜在竞态；常规场景下 `go test ./...` 即可。

## 安装

```shell
go get github.com/baabaaox/go-webrtcvad
```

## 使用示例

```go
package main

import (
    "bytes"
    "encoding/binary"
    "io"
    "log"
    "os"

    "github.com/baabaaox/go-webrtcvad"
)

const (
    VadMode       = 0
    SampleRate    = 16000
    BitDepth      = 16
    FrameDuration = 20
)

var (
    frameIndex  = 0
    frameSize   = SampleRate / 1000 * FrameDuration
    frameBuffer = make([]byte, SampleRate/1000*FrameDuration*BitDepth/8)
    frameActive = false
)

func main() {
    audioFile, err := os.Open("test.pcm")
    if err != nil {
        log.Fatal(err)
    }
    defer audioFile.Close()

    vadInst := webrtcvad.Create()
    defer webrtcvad.Free(vadInst)

    if err := webrtcvad.Init(vadInst); err != nil {
        log.Fatal(err)
    }
    if err := webrtcvad.SetMode(vadInst, VadMode); err != nil {
        log.Fatal(err)
    }

    chunkBuffer := &bytes.Buffer{}
    for {
        if _, err = audioFile.Read(frameBuffer); err == io.EOF {
            break
        } else if err != nil {
            log.Fatal(err)
        }

        frameActive, err = webrtcvad.Process(vadInst, SampleRate, frameBuffer, frameSize)
        if err != nil {
            log.Fatal(err)
        }
        if frameActive {
            chunkBuffer.Write(frameBuffer)
        }
        log.Printf("Frame: %d, Active: %t", frameIndex, frameActive)
        frameIndex++
    }

    if err := os.WriteFile("active.pcm", chunkBuffer.Bytes(), 0o644); err != nil {
        log.Fatal(err)
    }
}
```

