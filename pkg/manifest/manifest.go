package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest/schema2"
	"github.com/docker/distribution/reference"
	"github.com/docker/docker/api/server/httputils"
	_ "github.com/docker/docker/daemon/graphdriver/overlay2"
	distributiond "github.com/docker/docker/distribution"
	"github.com/docker/docker/image"
	refstore "github.com/docker/docker/reference"
)

const root = "/var/lib/docker"

func GenerateMetadata(imgGUN string, filePath string) error {
	img, tag, err := parseGUN(imgGUN)
	if err != nil {
		fmt.Printf("无法解析imgGUN：%v\n", err)
		return err
	}

	ref, err := httputils.RepoTagReference(img, tag)
	if err != nil {
		fmt.Printf("无法生成镜像引用：%v\n", err)
		return err
	}

	m, err := generateMetadata(ref)
	if err != nil {
		fmt.Printf("无法生成镜像元数据文件：%v\n", err)
		return err
	}

	err = os.WriteFile(filePath, m, 0644)
	if err != nil {
		fmt.Printf("无法保存到路径：%s，错误：%v\n", filePath, err)
		return err
	}
	output := filepath.Join(filePath, "output.dat")
	fmt.Printf("元数据文件生成成功，位于：%s\n", output)
	return nil
}

func generateMetadata(ref reference.NamedTagged) ([]byte, error) {
	referenceStore, imgStore, layerStore, err := prepare()
	if err != nil {
		fmt.Printf("本地文件系统服务启动失败，错误：%v\n", err)
		return []byte{}, err
	}

	imageID, err := referenceStore.Get(ref)
	if err != nil {
		fmt.Printf("无法获取对应镜像ID，错误：%v\n", err)
		return []byte{}, err
	}

	imgConfig, err := imgStore.Get(image.ID(imageID))
	if err != nil {
		fmt.Printf("无法获取对应镜像配置信息，错误：%v\n", err)
		return []byte{}, err
	}

	rootfs, err := rootFSFromConfig(imgConfig.RawJSON())
	if err != nil {
		fmt.Printf("无法获取对应镜像文件系统信息，错误：%v\n", err)
		return []byte{}, err
	}

	l, err := layerStore.Get(rootfs.ChainID())
	if err != nil {
		fmt.Printf("无法获取对应镜像的存储层，错误：%v\n", err)
		return []byte{}, err
	}
	defer l.Release()

	var dependencies []distribution.Descriptor
	for range rootfs.DiffIDs {
		ld, err := getDescriptor(l, string(l.DiffID()))
		if err != nil {
			fmt.Printf("镜像层描述符创建失败：%v\n", err)
			return []byte{}, err
		}
		l = l.Parent()
		dependencies = append(dependencies, ld)
	}
	dependencies = getCanonicalLayer(dependencies)

	configDescriptor, err := getConfigDescriptor(schema2.MediaTypeImageConfig, imgConfig.RawJSON())
	if err != nil {
		fmt.Printf("镜像配置描述符创建失败：%v\n", err)
		return []byte{}, err
	}

	m := schema2.Manifest{
		Versioned: schema2.SchemaVersion,
		Layers:    make([]distribution.Descriptor, len(dependencies)),
	}
	copy(m.Layers, dependencies)
	m.Config = configDescriptor
	manifest, err := schema2.FromStruct(m)
	if err != nil {
		fmt.Printf("获取镜像元数据文件格式转换：%v\n", err)
		return []byte{}, err
	}

	_, canonicalManifest, err := manifest.Payload()
	if err != nil {
		fmt.Printf("镜像元数据文件格式转换失败：%v\n", err)
	}
	return canonicalManifest, nil

}

func prepare() (refstore.Store, image.Store, distributiond.PushLayerProvider, error) {
	layerStores, layerStore, err := getLayerStore()
	if err != nil {
		fmt.Printf("无法创建镜像层服务：%v\n", err)
		return nil, nil, nil, err
	}

	imageRoot := filepath.Join(root, "image", layerStore.DriverName())
	ifs, err := image.NewFSStoreBackend(filepath.Join(imageRoot, "imagedb"))
	if err != nil {
		fmt.Printf("无法创建镜像文件系统：%v\n", err)
		return nil, nil, nil, err
	}

	refStoreLocation := filepath.Join(imageRoot, `repositories.json`)
	referenceStore, err := refstore.NewReferenceStore(refStoreLocation)
	if err != nil {
		fmt.Printf("无法获取引用存储文件：%v\n", err)
		return nil, nil, nil, err
	}

	imageStore, err := image.NewImageStore(ifs, layerStore)
	if err != nil {
		fmt.Printf("无法创建镜像存储服务：%v\n", err)
		return nil, nil, nil, err
	}

	return referenceStore, imageStore, layerStores, nil
}

func parseGUN(imgGUN string) (string, string, error) {
	parts := strings.Split(imgGUN, ":")
	if len(parts) != 2 {
		if len(parts) == 1 {
			fmt.Println("输入的GUN不包含tag，默认设置tag为latest！")
			repo := parts[0]
			tag := "latest"
			return repo, tag, nil
		} else if len(parts) == 3 {
			repo := parts[0] + ":" + parts[1]
			tag := parts[2]
			return repo, tag, nil
		} else {
			return "", "", fmt.Errorf("无法解析 GUN: %s", imgGUN)
		}
	}
	repo := parts[0]
	tag := parts[1]
	return repo, tag, nil
}
