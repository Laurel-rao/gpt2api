-- +goose Up
-- +goose StatementBegin
UPDATE `ecommerce_prompt_templates`
   SET `video_prompt` = '生成一支 5 秒精致电商商品短视频。
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

分秒脚本：每一秒都必须同时交代镜头、灯光、背景、声音和细节描述。
0-1 秒：镜头：商品主视觉开场，居中或三分构图，缓慢推近；灯光：柔和主光加边缘轮廓光；背景：干净电商场景或品牌色块；声音：轻柔开场音效或低音氛围铺底；细节描述：突出商品轮廓、材质、颜色和真实比例。
1-2 秒：镜头：切到核心卖点近景或轻微环绕；灯光：增强材质高光和反射层次；背景：保持同一视觉世界并加入少量空间层次；声音：加入细腻转场声或材质反馈音；细节描述：展示关键结构、纹理、接口、包装或使用方式。
2-3 秒：镜头：展示自然使用场景或功能演示；灯光：保持明亮可信，避免棚拍假感；背景：贴合目标平台和目标消费场景；声音：节奏轻微抬升；细节描述：用动作说明卖点如何解决需求，不编造额外功效。
3-4 秒：镜头：切到细节特写和短促可读的信息展示；灯光：聚焦商品重点部位；背景：干净有层次，避免杂乱；声音：使用清晰提示音或轻节拍；细节描述：不得编造价格、规格、Logo、认证或品牌承诺。
4-5 秒：镜头：形成电商海报式结束定帧，商品与核心利益点清晰；灯光：稳定高级，完成质感收束；背景：与前面镜头统一完整；声音：自然收束；细节描述：保持商品身份、比例、颜色、材质与需求一致。

画面限制：不要错误文字、错别字、水印、二维码、低清噪点、夸张变形或侵权品牌元素。
必须保持商品身份与需求一致：{{.CompactRequirement}}
{{.RetryExtraLine}}'
 WHERE `deleted_at` IS NULL
   AND (
     `video_prompt` LIKE '%镜头设计：开场商品主视觉亮相%'
     OR `video_prompt` LIKE '%运动要求：平滑推镜%'
   );
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
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
 WHERE `deleted_at` IS NULL
   AND `video_prompt` LIKE '%分秒脚本：每一秒都必须同时交代镜头、灯光、背景、声音和细节描述%';
-- +goose StatementEnd
