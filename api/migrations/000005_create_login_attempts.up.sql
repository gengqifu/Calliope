CREATE TABLE login_attempts (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    email      VARCHAR(100)    NOT NULL,
    ip         VARCHAR(45)     NOT NULL COMMENT 'IPv4 或 IPv6',
    success    TINYINT(1)      NOT NULL DEFAULT 0,
    created_at DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,

    PRIMARY KEY (id),
    KEY idx_email_created (email, created_at),
    KEY idx_ip_created    (ip, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='登录尝试记录（暴力破解检测）';
