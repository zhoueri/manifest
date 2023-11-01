package manifest

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest/schema2"
	distributiond "github.com/docker/docker/distribution"
	"github.com/docker/docker/distribution/xfer"
	"github.com/docker/docker/image"
	"github.com/docker/docker/layer"
	"github.com/docker/docker/pkg/idtools"
	"github.com/docker/docker/plugin"
	"github.com/opencontainers/go-digest"
)

const compressionBufSize = 32768

// 对描述符重新排序
func getCanonicalLayer(d []distribution.Descriptor) []distribution.Descriptor {
	canonicalLayers := []distribution.Descriptor{}
	for i := 0; i < len(d); i++ {
		canonicalLayers = append(canonicalLayers, d[len(d)-1-i])
	}
	return canonicalLayers
}

// 获取镜像配置文件的描述符
func getConfigDescriptor(configMediaType string, configJSON []byte) (distribution.Descriptor, error) {
	digester := digest.Canonical.Digester()
	teeReader := io.TeeReader(bytes.NewReader(configJSON), digester.Hash())

	_, err := io.ReadAll(teeReader) // 读取所有数据以触发哈希计算
	if err != nil {
		return distribution.Descriptor{}, err
	}

	desc := distribution.Descriptor{
		MediaType: configMediaType,
		Size:      int64(len(configJSON)),
		Digest:    digester.Digest(),
	}

	return desc, nil
}

// 获取镜像层的描述符
func getDescriptor(layer distributiond.PushLayer, id string) (distribution.Descriptor, error) {
	var reader io.ReadCloser

	contentReader, err := layer.Open()
	if err != nil {
		return distribution.Descriptor{}, retryOnError(err)
	}

	reader = contentReader

	switch m := layer.MediaType(); m {
	case schema2.MediaTypeUncompressedLayer:
		compressedReader, compressionDone := compress(reader)
		defer func(closer io.Closer) {
			closer.Close()
			<-compressionDone
		}(reader)
		reader = compressedReader
	case schema2.MediaTypeLayer:
	default:
		reader.Close()
		return distribution.Descriptor{}, xfer.DoNotRetry{Err: fmt.Errorf("unsupported layer media type %s", m)}
	}

	digester := digest.Canonical.Digester()
	tee := io.TeeReader(reader, digester.Hash())

	nn, err := getIOreaderSize(tee)

	if err != nil {
		return distribution.Descriptor{}, retryOnError(err)
	}

	desc := distribution.Descriptor{
		Digest:    digester.Digest(),
		MediaType: schema2.MediaTypeLayer,
		Size:      nn,
	}
	return desc, nil
}

type SizeCountingReader struct {
	Reader io.Reader
	Total  int64
}

func (scr *SizeCountingReader) Read(p []byte) (n int64, err error) {
	// fmt.Println("成功2！")
	nn, err := scr.Reader.Read(p)
	scr.Total += int64(nn)
	return int64(nn), err
}

// 根据io.reader获取镜像层文件的大小
func getIOreaderSize(in io.Reader) (int64, error) {
	reader := &SizeCountingReader{Reader: in}

	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n == 0 {
			break
		}
		if err != nil && err != io.EOF {
			fmt.Printf("Error: %v\n", err)
			return 0, err
		}
	}
	return reader.Total, nil
}

// 通过io.Reader对镜像层文件进行压缩
func compress(in io.Reader) (io.ReadCloser, chan struct{}) {
	compressionDone := make(chan struct{})

	pipeReader, pipeWriter := io.Pipe()
	bufWriter := bufio.NewWriterSize(pipeWriter, compressionBufSize)
	compressor := gzip.NewWriter(bufWriter)

	go func() {
		_, err := io.Copy(compressor, in)
		if err == nil {
			err = compressor.Close()
		}
		if err == nil {
			err = bufWriter.Flush()
		}
		if err != nil {
			pipeWriter.CloseWithError(err)
		} else {
			pipeWriter.Close()
		}
		close(compressionDone)
	}()

	return pipeReader, compressionDone
}

// 根据镜像的config数据获取文件系统
func rootFSFromConfig(c []byte) (*image.RootFS, error) {
	var unmarshalledConfig image.Image
	if err := json.Unmarshal(c, &unmarshalledConfig); err != nil {
		return nil, err
	}
	return unmarshalledConfig.RootFS, nil
}

// 获取镜像层本地服务
func getLayerStore() (distributiond.PushLayerProvider, layer.Store, error) {
	var graphOptions []string
	root := "/var/lib/docker"
	experimental := false
	driverName := ""
	idMapping := idtools.IdentityMapping{}
	pluginStore := plugin.NewStore()

	layerStore, err := layer.NewStoreFromOptions(layer.StoreOptions{
		Root:                      root,
		MetadataStorePathTemplate: filepath.Join(root, "image", "%s", "layerdb"),
		GraphDriver:               driverName,
		GraphDriverOptions:        graphOptions,
		IDMapping:                 idMapping,
		PluginGetter:              pluginStore,
		ExperimentalEnabled:       experimental,
	})
	if err != nil {
		return nil, nil, err
	}

	layerStores := distributiond.NewLayerProvidersFromStore(layerStore)

	return layerStores, layerStore, nil
}
