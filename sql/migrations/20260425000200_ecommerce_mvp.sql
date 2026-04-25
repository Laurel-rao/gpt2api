-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS `ecommerce_platforms` (
    `id`          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `code`        VARCHAR(64)     NOT NULL,
    `name`        VARCHAR(64)     NOT NULL,
    `field_schema` JSON           NULL,
    `remark`      VARCHAR(255)    NOT NULL DEFAULT '',
    `enabled`     TINYINT(1)      NOT NULL DEFAULT 1,
    `created_at`  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at`  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    `deleted_at`  DATETIME        NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_code` (`code`),
    KEY `idx_enabled` (`enabled`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='电商平台配置';

CREATE TABLE IF NOT EXISTS `ecommerce_prompt_templates` (
    `id`             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `name`           VARCHAR(64)     NOT NULL,
    `code`           VARCHAR(64)     NOT NULL,
    `content_prompt` TEXT            NOT NULL,
    `image_prompt`   TEXT            NOT NULL,
    `remark`         VARCHAR(255)    NOT NULL DEFAULT '',
    `enabled`        TINYINT(1)      NOT NULL DEFAULT 1,
    `created_at`     DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at`     DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    `deleted_at`     DATETIME        NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_code` (`code`),
    KEY `idx_enabled` (`enabled`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='电商提示词模板';

CREATE TABLE IF NOT EXISTS `ecommerce_style_templates` (
    `id`            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `name`          VARCHAR(64)     NOT NULL,
    `code`          VARCHAR(64)     NOT NULL,
    `style_prompt`  TEXT            NOT NULL,
    `layout_config` JSON            NULL,
    `remark`        VARCHAR(255)    NOT NULL DEFAULT '',
    `enabled`       TINYINT(1)      NOT NULL DEFAULT 1,
    `created_at`    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at`    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    `deleted_at`    DATETIME        NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_code` (`code`),
    KEY `idx_enabled` (`enabled`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='电商风格模板';

CREATE TABLE IF NOT EXISTS `ecommerce_tasks` (
    `id`                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `task_id`            VARCHAR(64)     NOT NULL,
    `user_id`            BIGINT UNSIGNED NOT NULL,
    `platform_id`        BIGINT UNSIGNED NOT NULL,
    `prompt_template_id` BIGINT UNSIGNED NOT NULL,
    `style_template_id`  BIGINT UNSIGNED NOT NULL,
    `requirement`        TEXT            NOT NULL,
    `reference_images`   JSON            NULL,
    `status`             VARCHAR(16)     NOT NULL DEFAULT 'queued',
    `progress`           INT             NOT NULL DEFAULT 0,
    `output_json`        JSON            NULL,
    `output_html`        MEDIUMTEXT      NULL,
    `error`              VARCHAR(1000)   NOT NULL DEFAULT '',
    `created_at`         DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `started_at`         DATETIME        NULL,
    `finished_at`        DATETIME        NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_task_id` (`task_id`),
    KEY `idx_user_created` (`user_id`, `created_at`),
    KEY `idx_status` (`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='电商生成任务';

CREATE TABLE IF NOT EXISTS `ecommerce_assets` (
    `id`            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `task_id`       VARCHAR(64)     NOT NULL,
    `asset_type`    VARCHAR(32)     NOT NULL,
    `image_task_id` VARCHAR(64)     NOT NULL DEFAULT '',
    `url`           VARCHAR(1024)   NOT NULL DEFAULT '',
    `file_id`       VARCHAR(255)    NOT NULL DEFAULT '',
    `prompt`        TEXT            NOT NULL,
    `status`        VARCHAR(16)     NOT NULL DEFAULT 'queued',
    `error`         VARCHAR(500)    NOT NULL DEFAULT '',
    `created_at`    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at`    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    KEY `idx_task` (`task_id`),
    KEY `idx_image_task` (`image_task_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='电商生成图片资产';

INSERT INTO `ecommerce_platforms` (`code`, `name`, `field_schema`, `remark`) VALUES
('generic', '通用电商',
 JSON_OBJECT('locale', 'zh-CN', 'currency', 'CNY', 'title_max', 60, 'description_max', 500, 'detail_width', 750, 'image_ratio', '1:1', 'required_assets', JSON_ARRAY('title_image', 'main_image', 'white_image', 'detail_image', 'price_image')),
 '适用于主流电商平台'),
('taobao', '淘宝/天猫',
 JSON_OBJECT('locale', 'zh-CN', 'currency', 'CNY', 'title_max', 60, 'description_max', 500, 'detail_width', 790, 'image_ratio', '1:1', 'required_assets', JSON_ARRAY('main_image', 'white_image', 'detail_image', 'price_image')),
 '淘宝、天猫商品主图与详情页字段'),
('jd', '京东',
 JSON_OBJECT('locale', 'zh-CN', 'currency', 'CNY', 'title_max', 60, 'description_max', 500, 'detail_width', 790, 'image_ratio', '1:1', 'required_assets', JSON_ARRAY('main_image', 'white_image', 'detail_image')),
 '京东 POP 商品详情字段'),
('pdd', '拼多多',
 JSON_OBJECT('locale', 'zh-CN', 'currency', 'CNY', 'title_max', 60, 'description_max', 400, 'detail_width', 750, 'image_ratio', '1:1', 'required_assets', JSON_ARRAY('main_image', 'price_image', 'detail_image')),
 '拼多多低价转化场景'),
('douyin', '抖音电商',
 JSON_OBJECT('locale', 'zh-CN', 'currency', 'CNY', 'title_max', 55, 'description_max', 300, 'detail_width', 750, 'image_ratio', '3:4', 'required_assets', JSON_ARRAY('main_image', 'price_image', 'detail_image')),
 '抖音商城和短视频挂车商品页'),
('xiaohongshu', '小红书店铺',
 JSON_OBJECT('locale', 'zh-CN', 'currency', 'CNY', 'title_max', 40, 'description_max', 600, 'detail_width', 750, 'image_ratio', '3:4', 'required_assets', JSON_ARRAY('main_image', 'detail_image')),
 '种草型商品内容'),
('amazon', 'Amazon',
 JSON_OBJECT('locale', 'en-US', 'currency', 'USD', 'title_max', 200, 'description_max', 2000, 'detail_width', 970, 'image_ratio', '1:1', 'required_assets', JSON_ARRAY('main_image', 'white_image', 'detail_image')),
 '跨境平台字段'),
('shopee', 'Shopee',
 JSON_OBJECT('locale', 'en-US', 'currency', 'USD', 'title_max', 120, 'description_max', 1500, 'detail_width', 750, 'image_ratio', '1:1', 'required_assets', JSON_ARRAY('main_image', 'price_image', 'detail_image')),
 '东南亚跨境平台'),
('shopify', 'Shopify',
 JSON_OBJECT('locale', 'en-US', 'currency', 'USD', 'title_max', 120, 'description_max', 1500, 'detail_width', 960, 'image_ratio', '4:5', 'required_assets', JSON_ARRAY('main_image', 'detail_image', 'price_image')),
 '独立站商品详情页')
ON DUPLICATE KEY UPDATE
 `name` = VALUES(`name`),
 `field_schema` = VALUES(`field_schema`),
 `remark` = VALUES(`remark`),
 `enabled` = 1;

INSERT INTO `ecommerce_prompt_templates` (`code`, `name`, `content_prompt`, `image_prompt`, `remark`) VALUES
('conversion', '转化优先',
 '突出购买理由、核心卖点、适用场景、信任背书和行动号召。文案结构按首屏吸引、卖点证明、场景联想、规格说明、下单理由组织，避免空泛形容词。',
 '画面必须服务转化，清晰展示商品、卖点文字和购买利益点。标题短句不超过 14 个中文字符，价格利益点必须醒目。',
 '默认营销模板'),
('premium', '质感品牌',
 '强调品牌感、材质、工艺、生活方式和高端可信赖表达。减少促销语气，突出质感、审美、耐用性和服务承诺。',
 '画面保持高级商业摄影质感，构图干净，文案克制，使用留白和材质细节体现价值。',
 '品牌型商品'),
('new_product', '新品首发',
 '围绕新品上市、首批体验、核心创新点和限时权益生成内容。突出新品差异、适合人群、首发福利和购买紧迫感。',
 '新品发布海报风格，商品主体居中，搭配新品标签、首发权益和核心创新点。',
 '新品上线活动'),
('live_sale', '直播带货',
 '使用直播间转化语言，突出限时福利、主播推荐、库存紧张、组合优惠和立即下单理由。语气直接但不夸大。',
 '直播间电商海报风格，高对比、强价格锚点、福利标签清晰，适合移动端浏览。',
 '直播和短视频带货'),
('cross_border', '跨境电商',
 '输出适合跨境平台的商品标题、五点卖点、使用场景和售后说明。语言简洁可信，避免无法证明的绝对化承诺。',
 '跨境平台商品图风格，白底主图规范、卖点信息清楚，适合 Amazon、Shopee、Shopify。',
 '跨境平台文案'),
('content_seed', '种草内容',
 '以真实体验、使用前后变化、生活场景和人群痛点组织内容。语气自然，强调为什么值得试。',
 '生活方式种草图，真实使用场景、自然光、轻量文字标注，适合小红书和内容电商。',
 '内容种草场景')
ON DUPLICATE KEY UPDATE
 `name` = VALUES(`name`),
 `content_prompt` = VALUES(`content_prompt`),
 `image_prompt` = VALUES(`image_prompt`),
 `remark` = VALUES(`remark`),
 `enabled` = 1;

INSERT INTO `ecommerce_style_templates` (`code`, `name`, `style_prompt`, `layout_config`, `remark`) VALUES
('clean', '清爽白底',
 '白色或浅灰背景，光线明亮，商品主体清晰，中文文字易读。适合白底图、主图和平台审核更严格的商品。',
 JSON_OBJECT('tone', 'clean', 'columns', 1, 'background', 'white', 'text_density', 'low', 'accent', '#111827'),
 '通用安全风格'),
('bold', '强促销',
 '高对比商业海报风格，价格利益点醒目，适合活动促销。使用红、黑、白形成强层级，按钮式行动号召清楚。',
 JSON_OBJECT('tone', 'bold', 'columns', 1, 'background', 'campaign', 'text_density', 'high', 'accent', '#ef4444'),
 '促销风格'),
('premium_dark', '高端深色',
 '深色商业摄影背景，局部高光，突出材质、轮廓和精致感。文字少量、精确、留白充足。',
 JSON_OBJECT('tone', 'premium', 'columns', 1, 'background', 'dark', 'text_density', 'low', 'accent', '#d6b46a'),
 '高客单价商品'),
('fresh_lifestyle', '生活方式',
 '真实家居或户外使用场景，自然光，人物只作为辅助，不遮挡商品。整体清新、可信、亲近。',
 JSON_OBJECT('tone', 'lifestyle', 'columns', 1, 'background', 'scene', 'text_density', 'medium', 'accent', '#16a34a'),
 '场景种草风格'),
('tech_minimal', '科技极简',
 '冷静科技感，干净背景，使用线性信息标注突出参数、功能和对比。适合数码、家电、工具类商品。',
 JSON_OBJECT('tone', 'tech', 'columns', 1, 'background', 'minimal', 'text_density', 'medium', 'accent', '#2563eb'),
 '数码科技风格'),
('cute_pastel', '柔和可爱',
 '柔和明亮配色，圆润元素，适合母婴、宠物、礼品和年轻女性消费品。画面亲和但信息清楚。',
 JSON_OBJECT('tone', 'pastel', 'columns', 1, 'background', 'soft', 'text_density', 'medium', 'accent', '#f472b6'),
 '柔和可爱风格')
ON DUPLICATE KEY UPDATE
 `name` = VALUES(`name`),
 `style_prompt` = VALUES(`style_prompt`),
 `layout_config` = VALUES(`layout_config`),
 `remark` = VALUES(`remark`),
 `enabled` = 1;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS `ecommerce_assets`;
DROP TABLE IF EXISTS `ecommerce_tasks`;
DROP TABLE IF EXISTS `ecommerce_style_templates`;
DROP TABLE IF EXISTS `ecommerce_prompt_templates`;
DROP TABLE IF EXISTS `ecommerce_platforms`;
-- +goose StatementEnd
