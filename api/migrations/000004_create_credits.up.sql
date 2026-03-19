CREATE TABLE credits (
    id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id      BIGINT UNSIGNED NOT NULL,
    date         DATE            NOT NULL COMMENT 'UTC+8 日期，每天零点重置',
    used         TINYINT         NOT NULL DEFAULT 0 COMMENT '已使用次数',
    limit_count  TINYINT         NOT NULL DEFAULT 5 COMMENT '当日上限',
    created_at   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    PRIMARY KEY (id),
    UNIQUE KEY uk_user_date (user_id, date),
    CONSTRAINT fk_credits_user FOREIGN KEY (user_id) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='每日生成额度表';
