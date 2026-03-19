CREATE TABLE tasks (
    id               BIGINT UNSIGNED  NOT NULL AUTO_INCREMENT,
    user_id          BIGINT UNSIGNED  NOT NULL,
    prompt           VARCHAR(200)     NOT NULL,
    lyrics           TEXT             DEFAULT NULL COMMENT 'NULL 表示 AI 自动生成歌词',
    mode             ENUM('vocal','instrumental') NOT NULL DEFAULT 'vocal',
    status           ENUM('queued','processing','completed','failed') NOT NULL DEFAULT 'queued',
    fail_reason      VARCHAR(500)     DEFAULT NULL,
    credit_date      DATE             NOT NULL COMMENT '扣减额度时的 UTC+8 日期，退款时按此日期回补',
    queue_position   INT              DEFAULT NULL COMMENT '入队时的位置（前方等待任务数）',
    candidate_a_key  VARCHAR(500)     DEFAULT NULL COMMENT '七牛云文件路径 key，候选 A',
    candidate_b_key  VARCHAR(500)     DEFAULT NULL COMMENT '七牛云文件路径 key，候选 B',
    duration_seconds INT              DEFAULT NULL COMMENT '生成音频时长（秒）',
    inference_ms     INT              DEFAULT NULL COMMENT '推理耗时（毫秒），用于监控',
    started_at       DATETIME         DEFAULT NULL COMMENT '开始推理时间',
    completed_at     DATETIME         DEFAULT NULL COMMENT '完成或失败时间',
    created_at       DATETIME         NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME         NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    PRIMARY KEY (id),
    KEY idx_user_id_created (user_id, created_at),
    KEY idx_status_started  (status, started_at) COMMENT '定时任务扫描超时任务用',
    CONSTRAINT fk_tasks_user FOREIGN KEY (user_id) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='音乐生成任务表';
