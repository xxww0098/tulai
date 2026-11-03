// 断点续传的元数据。
//
// 半成品文件是 dest.part，旁边放一个 dest.part.json 记录它属于哪个地址、
// 服务器给的校验值和完整长度。续传前先核对：地址不同、没有校验值的残片、
// 或者比声明的总长度还大，都直接丢掉，避免拼出一个混杂的文件。
package download

import (
	"encoding/json"
	"os"
)

// PartialMeta 是一个半成品属于哪个资源。
type PartialMeta struct {
	URL          string `json:"url"`
	SourceURL    string `json:"source_url,omitempty"` // 空值表示主地址
	FinalURL     string `json:"final_url,omitempty"`  // 重定向后的实际地址
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
	Total        int64  `json:"total"` // 小于 0 表示服务器没有给长度
}

func partialMetaPath(partPath string) string { return partPath + ".json" }

// LoadedPartial 是核对通过的半成品。
type LoadedPartial struct {
	Size int64
	Meta PartialMeta
}

// LoadPartial 核对半成品属于同一个资源。
// 已经收齐的文件即使没有校验值也可以复用；没收齐又认不出身份的残片会被删掉。
func LoadPartial(partPath, rawURL string) (*LoadedPartial, error) {
	return loadPartialForSources(partPath, rawURL, []string{rawURL})
}

func loadPartialForSources(partPath, rawURL string, sources []string) (*LoadedPartial, error) {
	// 空半成品也可保留已选备用地址；没有可拼接的数据，不要求校验值。
	info, err := os.Stat(partPath)
	if err != nil || info.IsDir() || (info.Size() == 0 && len(sources) <= 1) {
		return nil, nil
	}
	raw, err := os.ReadFile(partialMetaPath(partPath))
	if err != nil {
		_ = DiscardPartial(partPath)
		return nil, nil
	}
	var meta PartialMeta
	if json.Unmarshal(raw, &meta) != nil {
		_ = DiscardPartial(partPath)
		return nil, nil
	}
	complete := meta.Total > 0 && info.Size() == meta.Total
	noValidator := meta.ETag == "" && meta.LastModified == ""
	source := meta.SourceURL
	if source == "" {
		source = meta.URL
	}
	allowed := false
	for _, candidate := range sources {
		if candidate == source {
			allowed = true
			break
		}
	}
	if meta.URL != rawURL || !allowed || (noValidator && info.Size() > 0 && !complete) || (meta.Total > 0 && info.Size() > meta.Total) {
		_ = DiscardPartial(partPath)
		return nil, nil
	}
	return &LoadedPartial{Size: info.Size(), Meta: meta}, nil
}

// WritePartialMeta 原子地写半成品的元数据。权限收成 0600，里面有来源地址。
func WritePartialMeta(partPath string, meta PartialMeta) error {
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return writeAtomic(partialMetaPath(partPath), raw, 0o600)
}

// DiscardPartial 删掉半成品和它的元数据。文件不存在不算错。
func DiscardPartial(partPath string) error {
	err1 := os.Remove(partPath)
	err2 := os.Remove(partialMetaPath(partPath))
	if os.IsNotExist(err1) {
		err1 = nil
	}
	if os.IsNotExist(err2) {
		err2 = nil
	}
	if err1 != nil {
		return err1
	}
	return err2
}
