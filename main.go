package main

import (
	"fmt"

	_ "github.com/docker/docker/daemon/graphdriver/overlay2"
	"github.com/zhoueri/manifest_generator/pkg/manifest"
)

func main() {
	imgGUN := "192.168.128.69:8888/library/hello-world:latest"
	filePath := "/home/dct/output.dat"

	err := manifest.GenerateMetadata(imgGUN, filePath)
	if err != nil {
		fmt.Printf("元数据文件生成失败：%v", err)
		return
	}
}
