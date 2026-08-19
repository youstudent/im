// version.go：app_versions 表 DAO（客户端版本发布记录）。
package mysql

import (
	"database/sql"
	"errors"
	"time"
)

// AppVersion 客户端版本发布记录。
type AppVersion struct {
	ID           int64
	Version      string
	DownloadURL  string
	Sha256       string // 安装包 SHA-256（小写 hex），客户端自动更新下载后校验，防篡改
	ReleaseNotes string
	Publisher    string
	CreatedAt    time.Time
}

// CreateAppVersion 发布一个版本（version 唯一，重复发布报错）。
func (d *DB) CreateAppVersion(v *AppVersion) error {
	_, err := d.Exec(`INSERT INTO app_versions (id, version, download_url, sha256, release_notes, publisher)
		VALUES (?, ?, ?, ?, ?, ?)`,
		v.ID, v.Version, v.DownloadURL, v.Sha256, v.ReleaseNotes, v.Publisher)
	return err
}

// ListAppVersions 版本列表（按发布时间倒序，分页）。
func (d *DB) ListAppVersions(offset, limit int) ([]*AppVersion, error) {
	rows, err := d.Query(`SELECT id, version, download_url, sha256, release_notes, publisher, created_at
		FROM app_versions ORDER BY id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*AppVersion
	for rows.Next() {
		var v AppVersion
		if err := rows.Scan(&v.ID, &v.Version, &v.DownloadURL, &v.Sha256, &v.ReleaseNotes, &v.Publisher, &v.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, &v)
	}
	return list, rows.Err()
}

// CountAppVersions 版本总数。
func (d *DB) CountAppVersions() (int64, error) {
	var n int64
	err := d.QueryRow(`SELECT COUNT(1) FROM app_versions`).Scan(&n)
	return n, err
}

// GetLatestAppVersion 最新版本（最后发布的一条）；无记录时返回 (nil, nil)。
func (d *DB) GetLatestAppVersion() (*AppVersion, error) {
	row := d.QueryRow(`SELECT id, version, download_url, sha256, release_notes, publisher, created_at
		FROM app_versions ORDER BY id DESC LIMIT 1`)
	var v AppVersion
	if err := row.Scan(&v.ID, &v.Version, &v.DownloadURL, &v.Sha256, &v.ReleaseNotes, &v.Publisher, &v.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &v, nil
}
