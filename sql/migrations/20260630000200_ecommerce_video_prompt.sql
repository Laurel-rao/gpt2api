-- +goose Up
ALTER TABLE `ecommerce_prompt_templates`
  ADD COLUMN `video_prompt` TEXT NULL AFTER `image_prompt`;

UPDATE `ecommerce_prompt_templates`
   SET `video_prompt` = '生成一支精致的电商商品短视频。
目标平台：{{.Platform.Name}}
生成语言：{{.LanguageName}}（{{.LanguageCode}}）
可见文字语言：{{.CompactLanguageRule}}
商品标题：{{.ProductTitle}}
核心价值：{{.ProductCoreValue}}
卖点：{{.SellingPointsText}}
规格：{{.KeySpecsText}}
营销方向：{{.MarketingCopyText}}
价格/促销：{{.PriceText}}
视觉风格：{{.VisualDirection}}
镜头设计：开场商品主视觉亮相，中段展示自然使用场景与细节特写，结尾形成清晰的电商海报式定帧。
运动要求：平滑推镜、轻微产品转动或层次视差，光线高级，画面稳定，不闪烁。
文字要求：只使用商品资料中的短促可读营销文字；不得编造价格、规格、Logo 或品牌承诺。
必须保持商品身份与需求一致：{{.CompactRequirement}}
{{.RetryExtraLine}}'
 WHERE COALESCE(`video_prompt`, '') = '';

ALTER TABLE `ecommerce_prompt_templates`
  MODIFY COLUMN `video_prompt` TEXT NOT NULL;

-- +goose Down
ALTER TABLE `ecommerce_prompt_templates`
  DROP COLUMN `video_prompt`;
