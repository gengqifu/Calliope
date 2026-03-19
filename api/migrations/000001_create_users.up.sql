CREATE TABLE users (
    id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    email        VARCHAR(100)    NOT NULL,
    password     VARCHAR(60)     NOT NULL COMMENT 'bcrypt hash，固定 60 字符',
    nickname     VARCHAR(50)     NOT NULL DEFAULT '' COMMENT '显示名称，默认空',
    status       TINYINT         NOT NULL DEFAULT 1 COMMENT '1=active, 0=banned',
    created_at   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    PRIMARY KEY (id),
    UNIQUE KEY uk_email (email)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户表';
