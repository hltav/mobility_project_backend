// package cache

// import (
// 	"crypto/sha1"
// 	"encoding/json"
// 	"fmt"
// 	"io/ioutil"
// 	"os"
// 	"path/filepath"
// 	"time"
// )

// var basePath string

// // InitFromEnv inicializa o diretório de armazenamento do cache.
// // Usa VALKEY_STORAGE_PATH ou /tmp/valkey por padrão.
// func InitFromEnv() error {
// 	p := os.Getenv("VALKEY_STORAGE_PATH")
// 	if p == "" {
// 		p = "/tmp/valkey"
// 	}
// 	basePath = p
// 	if err := os.MkdirAll(basePath, 0o755); err != nil {
// 		return fmt.Errorf("criar dir cache: %w", err)
// 	}
// 	return nil
// }

// type stored struct {
// 	Expires int64           `json:"expires"`
// 	Data    json.RawMessage `json:"data"`
// }

// func keyToFile(key string) string {
// 	h := sha1.Sum([]byte(key))
// 	return filepath.Join(basePath, fmt.Sprintf("%x.json", h))
// }

// // SetJSON grava um valor serializado com TTL.
// func SetJSON(key string, v any, ttl time.Duration) error {
// 	if basePath == "" {
// 		// não inicializado
// 		return nil
// 	}
// 	b, err := json.Marshal(v)
// 	if err != nil {
// 		return err
// 	}
// 	s := stored{
// 		Expires: time.Now().Add(ttl).Unix(),
// 		Data:    b,
// 	}
// 	out, err := json.Marshal(s)
// 	if err != nil {
// 		return err
// 	}
// 	fn := keyToFile(key)
// 	return ioutil.WriteFile(fn, out, 0o644)
// }

// // GetJSON tenta ler e desserializar do cache. Retorna (true,nil) quando hit.
// func GetJSON(key string, dst any) (bool, error) {
// 	if basePath == "" {
// 		return false, nil
// 	}
// 	fn := keyToFile(key)
// 	b, err := ioutil.ReadFile(fn)
// 	if err != nil {
// 		return false, nil
// 	}
// 	var s stored
// 	if err := json.Unmarshal(b, &s); err != nil {
// 		// corrupção: remove
// 		_ = os.Remove(fn)
// 		return false, nil
// 	}
// 	if time.Now().Unix() > s.Expires {
// 		_ = os.Remove(fn)
// 		return false, nil
// 	}
// 	if err := json.Unmarshal(s.Data, dst); err != nil {
// 		return false, err
// 	}
// 	return true, nil
// }

// // Delete apaga uma chave do cache.
//
//	func Delete(key string) error {
//		if basePath == "" {
//			return nil
//		}
//		fn := keyToFile(key)
//		return os.Remove(fn)
//	}
package cache

import (
	"context"
	"encoding/json"
	"time"
)

// SetJSON serializa v como JSON e armazena no Valkey com TTL.
func (c *Client) SetJSON(ctx context.Context, key string, v any, ttl time.Duration) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.Set(ctx, key, b, ttl)
}

// GetJSON recupera e desserializa um valor do Valkey.
// Retorna (false, nil) se a chave não existir (cache miss).
func (c *Client) GetJSON(ctx context.Context, key string, dst any) (bool, error) {
	b, err := c.Get(ctx, key)
	if err != nil {
		return false, err
	}
	if b == nil {
		return false, nil
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return false, err
	}
	return true, nil
}
