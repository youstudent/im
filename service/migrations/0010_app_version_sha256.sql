-- 审计 P1（供应链安全）：客户端自动更新下载安装包后需校验 SHA-256，防篡改/恶意包静默执行。
-- 发布版本时必填；旧版本记录 sha256 为空串，客户端对无摘要的更新拒绝自动安装（可手动下载）。

ALTER TABLE app_versions ADD COLUMN sha256 VARCHAR(64) NOT NULL DEFAULT '' COMMENT '安装包 SHA-256（小写 hex，客户端下载后校验）' AFTER download_url;
