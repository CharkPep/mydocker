package ImageManager

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const DOCKER_HUB = "registry.hub.docker.com"

type ImageManager struct {
	auth Authenticator
	mu   sync.Mutex
}

const (
	MANIFEST           = "application/vnd.docker.distribution.manifest.v2+json"
	WITHLAYERSINCLUDED = "application/vnd.oci.image.index.v1+json"
)

type Manifest struct {
	// TODO: Add support for nested index manifests
	MediaType     string `json:"mediaType"`
	SchemaVersion int    `json:"schemaVersion"`
	Digest        string `json:"digest"`
	Size          int    `json:"size"`
	Platform      struct {
		Architecture string `json:"architecture"`
		OS           string `json:"os"`
	} `json:"platform"`
}

type ImageIndexManifest struct {
	SchemaVersion int        `json:"schemaVersion"`
	MediaType     string     `json:"mediaType"`
	Manifest      []Manifest `json:"manifests"`
	Layers        []Layer    `json:"layers"`
}

type Layer struct {
	MediaType string `json:"mediaType"`
	Size      int    `json:"size"`
	Digest    string `json:"digest"`
}

type LayersManifest struct {
	SchemaVersion int     `json:"schemaVersion"`
	MediaType     string  `json:"mediaType"`
	Config        Layer   `json:"config"`
	Layers        []Layer `json:"layers"`
}

func NewImageManager(auth Authenticator) *ImageManager {
	return &ImageManager{
		auth: auth,
	}
}

func (m *ImageManager) getIndexManifest(ctx context.Context, image, tag, token string) (*ImageIndexManifest, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("https://%s/v2/library/%s/manifests/%s", DOCKER_HUB, image, tag), nil)
	if err != nil {
		return nil, err
	}
	if err = m.auth.Authenticate(ctx, req, image, PULL); err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	client := http.DefaultClient
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var manifest ImageIndexManifest
	err = json.NewDecoder(res.Body).Decode(&manifest)
	if err != nil {
		return nil, err
	}
	return &manifest, nil
}

func findArchitectureSpecificDigest(manifest ImageIndexManifest) (string, error) {
	for _, layer := range manifest.Manifest {
		if layer.Platform.Architecture == runtime.GOARCH && layer.Platform.OS == runtime.GOOS {
			return layer.Digest, nil
		}
	}
	return "", fmt.Errorf("No matching layer found for %s/%s", runtime.GOARCH, runtime.GOOS)
}

func (m *ImageManager) getLayers(ctx context.Context, image, digest string) (*[]Layer, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("https://%s/v2/library/%s/manifests/%s", DOCKER_HUB, image, digest), nil)
	if err != nil {
		return nil, err
	}
	if err = m.auth.Authenticate(ctx, req, image, PULL); err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")
	client := http.DefaultClient
	res, err := client.Do(req)
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Error fetching layers: %s", res.Status)
	}
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var manifest LayersManifest
	err = json.NewDecoder(res.Body).Decode(&manifest)
	if err != nil {
		return nil, err
	}
	return &manifest.Layers, nil
}

// TODO Implement image caching
func (m *ImageManager) downloadLayer(ctx context.Context, image, rootDir string, digest string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("https://%s/v2/library/%s/blobs/%s", DOCKER_HUB, image, digest), nil)
	if err != nil {
		return err
	}
	if err = m.auth.Authenticate(ctx, req, image, PULL); err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.oci.images.layer.v1.tar")
	client := http.DefaultClient
	fmt.Printf("Downloading layer %s\n", digest)
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	defer res.Body.Close()
	file, err := os.Create(path.Join(rootDir, digest+".tar"))
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, res.Body)

	return err
}

func extractTar(rootDir string) error {
	files, err := os.ReadDir(rootDir)
	if err != nil {
		return err
	}
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".tar") {
			tarPath := filepath.Join(rootDir, file.Name())
			fmt.Printf("Extracting %s\n", tarPath)
			cmd := exec.Command("tar", "-xvf", tarPath, "-C", rootDir)
			cmd.Stderr = os.Stderr
			if err = cmd.Run(); err != nil {
				return err
			}
			if err = os.Remove(tarPath); err != nil {
				return err
			}
		}
	}

	return nil
}

func (m *ImageManager) Pull(image, tag, path string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manifest, err := m.getIndexManifest(ctx, image, tag, "")
	if err != nil {
		return err
	}
	var imageLayers *[]Layer
	if manifest.Layers == nil {
		imageDigest, err := findArchitectureSpecificDigest(*manifest)
		if err != nil {
			return err
		}

		imageLayers, err = m.getLayers(ctx, image, imageDigest)
		if err != nil {
			return err
		}
	} else {
		imageLayers = &manifest.Layers
	}
	errChan := make(chan error)
	for _, layer := range *imageLayers {
		go func(ctx context.Context, image, path string, layer Layer) {
			err := m.downloadLayer(ctx, image, path, layer.Digest)
			if err != nil {
				errChan <- err

			}
			errChan <- nil
		}(ctx, image, path, layer)

		for i := 0; i < len(*imageLayers); i++ {
			select {
			case err := <-errChan:
				if err != nil {
					return err
				}
			}
		}
	}

	if err = extractTar(path); err != nil {
		return err
	}
	return nil
}
