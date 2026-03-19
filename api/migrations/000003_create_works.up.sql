CREATE TABLE works (
    id               BIGINT UNSIGNED  NOT NULL AUTO_INCREMENT,
    user_id          BIGINT UNSIGNED  NOT NULL,
    task_id          BIGINT UNSIGNED  NOT NULL COMMENT '来源任务',
    title            VARCHAR(50)      NOT NULL COMMENT '作品名称',
    prompt           VARCHAR(200)     NOT NULL COMMENT '冗余存储，避免查询作品列表时 JOIN tasks',
    mode             ENUM('vocal','instrumental') NOT NULL,
    audio_key        VARCHAR(500)     NOT NULL COMMENT '七牛云文件路径 key',
    duration_seconds INT              NOT NULL DEFAULT 0,
    play_count       INT UNSIGNED     NOT NULL DEFAULT 0,
    deleted_at       DATETIME         DEFAULT NULL COMMENT '软删除时间',
    created_at       DATETIME         NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME         NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    PRIMARY KEY (id),
    KEY idx_user_id_created (user_id, created_at),
    UNIQUE KEY uk_task_id (task_id) COMMENT '一个任务只能保存一条作品',
    CONSTRAINT fk_works_user FOREIGN KEY (user_id) REFERENCES users (id),
    CONSTRAINT fk_works_task FOREIGN KEY (task_id) REFERENCES tasks (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户保存的作品表';
