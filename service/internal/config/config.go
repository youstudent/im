// Package config 负责加载与校验配置（直接读取 config.yaml）。
package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config 服务端全部配置。
type Config struct {
	Server  Server  `yaml:"server"`
	MySQL   MySQL   `yaml:"mysql"`
	Redis   Redis   `yaml:"redis"`
	OSS     OSS     `yaml:"oss"`
	JWT     JWT     `yaml:"jwt"`
	Log     Log     `yaml:"log"`
	Gateway Gateway `yaml:"gateway"`
}

// Server HTTP 服务配置。
type Server struct {
	Name     string `yaml:"name"`
	HTTPAddr string `yaml:"http_addr"` // HTTP + WS 监听地址，如 :8080
	Mode     string `yaml:"mode"`      // debug / release
}

// MySQL 连接池配置。
type MySQL struct {
	DSN            string `yaml:"dsn"`
	MaxOpenConns   int    `yaml:"max_open_conns"`
	MaxIdleConns   int    `yaml:"max_idle_conns"`
	ConnMaxLifetime int   `yaml:"conn_max_lifetime"` // 秒
	MigrateDir     string `yaml:"migrate_dir"`
}

// Redis 客户端配置。
type Redis struct {
	Addr         string `yaml:"addr"`
	Password     string `yaml:"password"`
	DB           int    `yaml:"db"`
	PoolSize     int    `yaml:"pool_size"`
	DialTimeout  int    `yaml:"dial_timeout"` // 秒
	ReadTimeout  int    `yaml:"read_timeout"`
	WriteTimeout int    `yaml:"write_timeout"`
}

// OSS 阿里云对象存储配置。
type OSS struct {
	Endpoint        string `yaml:"endpoint"`
	AccessKeyID     string `yaml:"access_key_id"`
	AccessKeySecret string `yaml:"access_key_secret"`
	Bucket          string `yaml:"bucket"`
	Region          string `yaml:"region"`
	// PresignExpireSec 预签名 URL 有效期（秒）。
	PresignExpireSec int `yaml:"presign_expire_sec"`
}

// JWT 配置。
type JWT struct {
	Secret         string `yaml:"secret"`
	AccessExpire   int    `yaml:"access_expire"`   // 秒
	RefreshExpire  int    `yaml:"refresh_expire"`  // 秒
	Issuer         string `yaml:"issuer"`
}

// Log 日志配置。
type Log struct {
	Level  string `yaml:"level"`
	Output string `yaml:"output"`
}

// Gateway WebSocket 网关配置。
type Gateway struct {
	HeartbeatInterval int `yaml:"heartbeat_interval"` // 秒
	WriteWait         int `yaml:"write_wait"`         // 秒
	PongWait          int `yaml:"pong_wait"`          // 秒
}

// Load 从指定路径加载配置，缺失关键字段时返回错误。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Server.HTTPAddr == "" {
		c.Server.HTTPAddr = ":8080"
	}
	if c.Server.Mode == "" {
		c.Server.Mode = "debug"
	}
	if c.MySQL.MaxOpenConns == 0 {
		c.MySQL.MaxOpenConns = 20
	}
	if c.MySQL.MaxIdleConns == 0 {
		c.MySQL.MaxIdleConns = 10
	}
	if c.MySQL.ConnMaxLifetime == 0 {
		c.MySQL.ConnMaxLifetime = 3600
	}
	if c.MySQL.MigrateDir == "" {
		c.MySQL.MigrateDir = "migrations"
	}
	if c.Redis.PoolSize == 0 {
		c.Redis.PoolSize = 20
	}
	if c.Redis.DialTimeout == 0 {
		c.Redis.DialTimeout = 5
	}
	if c.Redis.ReadTimeout == 0 {
		c.Redis.ReadTimeout = 3
	}
	if c.Redis.WriteTimeout == 0 {
		c.Redis.WriteTimeout = 3
	}
	if c.OSS.PresignExpireSec == 0 {
		c.OSS.PresignExpireSec = 600
	}
	if c.JWT.AccessExpire == 0 {
		c.JWT.AccessExpire = 7200
	}
	if c.JWT.RefreshExpire == 0 {
		c.JWT.RefreshExpire = 30 * 24 * 3600
	}
	if c.JWT.Issuer == "" {
		c.JWT.Issuer = "im-service"
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	if c.Gateway.HeartbeatInterval == 0 {
		c.Gateway.HeartbeatInterval = 30
	}
	if c.Gateway.WriteWait == 0 {
		c.Gateway.WriteWait = 10
	}
	if c.Gateway.PongWait == 0 {
		c.Gateway.PongWait = 60
	}
}

func (c *Config) validate() error {
	if c.MySQL.DSN == "" {
		return errMissing("mysql.dsn")
	}
	if c.Redis.Addr == "" {
		return errMissing("redis.addr")
	}
	if c.JWT.Secret == "" {
		return errMissing("jwt.secret")
	}
	return nil
}

func errMissing(field string) error {
	return &ConfigError{Field: field}
}

// ConfigError 配置缺失错误。
type ConfigError struct{ Field string }

func (e *ConfigError) Error() string { return "config: missing required field " + e.Field }
