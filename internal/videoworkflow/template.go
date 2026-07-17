package videoworkflow

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed templates/ancient_drama_v1.json
var ancientDramaV1JSON []byte

func BuiltinTemplateV1() (Template, error) {
	var template Template
	if err := json.Unmarshal(ancientDramaV1JSON, &template); err != nil {
		return Template{}, fmt.Errorf("decode embedded video workflow template: %w", err)
	}
	return template, nil
}

func MustBuiltinTemplateV1() Template {
	template, err := BuiltinTemplateV1()
	if err != nil {
		panic(err)
	}
	return template
}

// BuiltinTemplates 返回当前可创建的模板。保留 v1 解码入口仅用于读取历史快照；
// 新建工作流统一使用 Graph v2。
func BuiltinTemplates() ([]Template, error) {
	legacy, err := BuiltinTemplateV1()
	if err != nil {
		return nil, err
	}
	ancient, err := upgradeAncientTemplate(legacy)
	if err != nil {
		return nil, err
	}
	globalChairman, err := globalChairmanDramaTemplate()
	if err != nil {
		return nil, err
	}
	blank := blankVideoCanvasTemplate()
	return []Template{ancient, globalChairman, blank}, nil
}

func upgradeAncientTemplate(template Template) (Template, error) {
	graph, err := UpgradeGraphV1ToV2(template.Graph)
	if err != nil {
		return Template{}, err
	}
	graph.Settings.Resolution = Resolution1080p
	graph.Settings.VideoModel = "doubao-seedance-2-0-fast-260128"
	graph.Settings.ImageModel = "gpt-image-2"
	if err := applyAncientDramaV4Defaults(&graph); err != nil {
		return Template{}, err
	}
	NormalizeBackgroundEnvironmentPorts(&graph)
	template.Code = "ancient_drama_seedance"
	template.Version = 4
	template.Name = "古风多角色短剧 · Seedance 材料包"
	template.Description = "先产出故事圣经、人物小传、场景设定、分幕剧情和分镜镜头，再按角色定妆、背景、Seedance 15 秒场景视频和时间线合成生成古风短剧。"
	template.Graph = graph
	return template, nil
}

func globalChairmanDramaTemplate() (Template, error) {
	settings := Settings{
		AspectRatio: AspectRatioPortrait, Resolution: Resolution1080p, FPS: DefaultFPS,
		SceneDurationMS: SceneDurationMS, CharacterApprovalPolicy: ApprovalAutoFirst,
		StoryboardApprovalPolicy: ApprovalAuto, TextModel: "default", ImageModel: "gpt-image-2", VideoModel: "doubao-seedance-2-0-fast-260128",
	}
	timelineConfig, err := json.Marshal(TimelineConfig{Clips: []TimelineClip{
		{ID: "clip_1", SourceNodeID: "video_1", SourcePort: "video", TrimOutMS: 7_500},
		{ID: "clip_2", SourceNodeID: "video_2", SourcePort: "video", TrimOutMS: 7_500},
		{ID: "clip_3", SourceNodeID: "video_3", SourcePort: "video", TrimOutMS: 7_500},
		{ID: "clip_4", SourceNodeID: "video_4", SourcePort: "video", TrimOutMS: 7_500},
	}})
	if err != nil {
		return Template{}, err
	}
	var configErr error
	config := func(fields map[string]any) json.RawMessage {
		if configErr != nil {
			return nil
		}
		encoded, err := json.Marshal(fields)
		if err != nil {
			configErr = err
			return nil
		}
		return encoded
	}
	characterOutputs := []Port{{ID: "candidates", Type: PortImageSet}, {ID: "selected", Type: PortImage}}
	characterInputs := []Port{
		{ID: "brief", Type: PortText, Required: true},
		{ID: "character_plan", Label: "角色生成资产", Type: PortText, Required: true},
	}
	scriptInputs := []Port{
		{ID: "brief", Type: PortText, Required: true},
		{ID: "outline", Label: "大纲资产", Type: PortText, Required: true},
		{ID: "character_plan", Label: "角色资产", Type: PortText, Required: true},
		{ID: "world", Label: "世界观/背景资产", Type: PortText, Required: true},
		{ID: "director", Label: "Seedance2导演审核", Type: PortText, Required: true},
		{ID: "hero", Type: PortImage, Required: true},
		{ID: "heroine", Type: PortImage, Required: true},
		{ID: "cousin", Type: PortImage, Required: true},
		{ID: "rival", Type: PortImage, Required: true},
	}
	videoInputs := []Port{
		{ID: "scene", Type: PortScene, Required: true},
		{ID: "director", Label: "导演审核资产", Type: PortText, Required: true},
		{ID: "background", Type: PortImage, Required: true},
		{ID: "hero", Type: PortImage, Required: true},
		{ID: "heroine", Type: PortImage, Required: true},
		{ID: "cousin", Type: PortImage, Required: true},
		{ID: "rival", Type: PortImage, Required: true},
	}
	const characterSheetSuffix = "输出角色参考/定妆图，不是剧情镜头：上排全身正面、侧面、背面三视图；中排脸部大特写正面与3/4侧，统一五官、发型、眼神和气质；下排服装与物料细节图，包含面料纹理、领口、徽章、配饰、鞋履、腰带、文件夹或权杖等材质特写；浅底设定稿、布局清晰。所有角色均为虚构成年人；非照片；禁止写实真人脸、真人照片、真实政治人物。"
	outlinePrompt := "大纲资产：输出《千年权臣：我在纽港执掌寰球联合会》第1集的短剧大纲和30秒节拍。必须写清：1）一句话钩子：毒酒赐死穿越到纽港寰球联合会门口；2）四段7.5秒剧情：落地被嘲、受辱夜学、危机会议识人反杀、主席台金句重逢；3）每段爽点、反转点、情绪推进和结尾钩子；4）古人核心竞争力如何推动剧情：识人术、纵横术、朝堂辩论、乱世治理。要求狗血短剧化，近现实架空名只用米国、罗斯国、东瀛国、华国、纽港、鹰宫、五环楼、寰球联合会。禁止真实政治人物、真实机构名、文字/字幕/LOGO/水印。"
	characterPlanPrompt := "角色生成资产：为4个角色输出可直接供生图节点使用的定妆规范。谢无咎：大雍权臣，旧黑朝服、血痕、束冠长发，核心技能识人术/纵横术/朝堂辩论/乱世治理；萧明凰：赐死男主的女帝，玄金凤纹长袍与现代黑大衣，追悔火葬场；林晚棠：华国青年外交官，短发深蓝西装、工作证和平板，发现男主合纵图；亚伦·霍克：米国代表，银灰西装鹰形领针，嘲讽后被打脸。每个角色都要写外形、服装材质、表情、表演方式、禁用项。角色均为虚构成年人；非照片；禁止写实真人脸和真实政治人物。"
	worldPrompt := "世界观/剧情背景资产：输出近现实架空世界设定和四个场景首帧规范。国家/机构名必须架空：米国、罗斯国、东瀛国、华国、纽港、鹰宫、五环楼、寰球联合会。场景1纽港雨夜寰联总部外广场：玻璃幕墙、万国旗、警戒栏、手机直播光点；场景2救助站与地下公共图书馆：旧长椅、自动售货机冷光、国际法书、地图；场景3寰球联合会危机会议厅：环形会议桌、同传耳机、蓝色席牌、世界地图屏；场景4主席台：弧形万国旗、演讲台、聚光灯、记者席闪光。背景图只做图生视频首帧/场景参考，无人空镜或远景剪影，禁止可辨识人物。"
	directorPrompt := "Seedance2导演审核资产：先按创意总监逻辑审核，再给脚本和视频节点执行。必须产出：source_diagnosis、creative_directions 2-3项、selected_direction、reference_terms_used、compatibility_check、creative_audit。reference_terms_used 只能从词库选择：推镜头、拉镜头、跟拍、环绕拍摄、希区柯克变焦、快切、硬切、闪回、叠化、交叉蒙太奇、电影感、胶片质感、HDR、3D 国漫 CG、冷色调、高对比度、硬光剪影、镜头光晕。creative_audit 四项必须 pass：记忆点、意外感、情绪、叙事；不通过就重写，不输出平庸 prompt。"
	scriptPrompt := "你是 Seedance 视频创意总监，不是模板填充器。基于上游题材材料和4张角色参考图，先做素材/文案诊断，再做2-3个完全不同的创意方向，选择最有记忆点的一版，完成运镜匹配、搭配验证和创意审核。只输出严格 JSON，不要 Markdown。顶层必须包含 title、source_diagnosis、creative_directions、selected_direction、reference_terms_used、compatibility_check、creative_audit、scenes。creative_directions 每项包含 id、logline、memory_point、unexpected_detail、emotion_arc、risk。selected_direction 必须说明为什么选它。reference_terms_used 只能从 reference.md 词库取词，至少包含：推镜头、拉镜头、跟拍、环绕拍摄、希区柯克变焦、快切、硬切、闪回、叠化、交叉蒙太奇、电影感、胶片质感、HDR、3D 国漫 CG、冷色调、高对比度、硬光剪影、镜头光晕。compatibility_check 必须逐项判断“角色图 + 场景背景 + prompt + 运镜”是否协调，不协调要在 JSON 内给出修正后的 prompt。creative_audit 必须包含 memory_point、unexpectedness、emotion、narrative 四项，值必须为 pass，并写明通过理由。scenes 必须恰好4项，每项 duration_seconds=15、effective_trim_ms=7500，并包含 index、scene_id、title、location_id、characters、dramatic_beat、ancient_competency、shots、dialogue、audio、image_prompt、seedance_prompt。每个 seedance_prompt 必须中文、可直接给即梦使用，0-7.5秒完成有效剧情动作，7.5-15秒做余韵；台词用“角色（情绪）：台词”。必须使用近现实架空名：米国、罗斯国、东瀛国、华国、纽港、鹰宫、五环楼、寰球联合会；不得出现真实国家机构、真实政治人物。禁止文字/字幕/LOGO/水印；禁止写实真人脸、真人照片、角色换脸、参考图拼贴布局入镜。"
	backgroundPrompt := func(sceneTitle, sceneBrief string) string {
		return fmt.Sprintf("读取上游 scenes 对应项的 image_prompt、location_id、lighting、props，只提取地点、时间、光影、空间布局、静物道具和氛围，生成“%s”的图生视频首帧/场景参考图。%s。9:16 单镜头、非拼贴、电影感、3D 国漫 CG、HDR、冷色调与高对比度；无人空镜或仅远景不可辨识剪影；禁止近中景可辨识人物、肢体动作和表演；禁止文字/字幕/LOGO/水印；非照片；禁止写实真人脸、真人照片、真实政治人物。", sceneTitle, sceneBrief)
	}
	videoPrompt := func(index int, sceneTitle, fallbackBeat string) string {
		return fmt.Sprintf("9:16，30fps，15秒，Seedance 2.0，电影感、3D 国漫 CG、HDR。读取上游 scene_%d 的 scenes[%d].seedance_prompt，并结合 selected_direction、creative_audit、reference_terms_used、compatibility_check 生成最终即梦中文 prompt；如果上游缺失，就围绕“%s”重写，但必须保留本集核心竞争力：谢无咎用识人术、纵横术、朝堂辩论和乱世治理经验完成现代降维打击。参考图用途：@图片1为场景背景参考，@图片2为男主谢无咎角色参考，@图片3为女帝萧明凰角色参考，@图片4为林晚棠角色参考，@图片5为亚伦·霍克角色参考。0-7.5秒必须完成有效剧情：%s；7.5-15秒只做表情余韵、灯光变化或悬念停顿，方便时间线裁前7.5秒合成30秒。只能使用 reference_terms_used 中的运镜/风格词，不自造镜头词；台词用“角色（情绪）：台词”；音效写环境音、音桥或无声处理。禁止文字/字幕/LOGO/水印；禁止写实真人脸、真人照片、真实政治人物、角色换脸、参考图拼贴布局入镜。", index, index-1, sceneTitle, fallbackBeat)
	}
	graph := Graph{SchemaVersion: SchemaVersionV2, Settings: settings}
	graph.Nodes = []Node{
		{
			ID: "brief", Type: NodeStoryBrief, Position: Position{X: 40, Y: 245}, PositionMode: PositionAuto,
			Config: config(map[string]any{
				"title":  "Seedance2 创意工作台简报",
				"prompt": "创建分集短剧《千年权臣：我在纽港执掌寰球联合会》第1集《古装疯子闯寰联》的 9:16、约30秒单集素材。不要套模板，按 seedance2 创意总监流程执行：素材/题材诊断 -> 创意发散 -> 选择最有记忆点方向 -> 文案扩写 -> 运镜匹配 -> 图+prompt+运镜搭配验证 -> 创意审核。题材要求狗血短剧化、强反转、强打脸、结尾钩子；近现实架空命名：米国、罗斯国、东瀛国、华国、纽港、鹰宫、五环楼、寰球联合会。男主谢无咎是大雍权臣，被女帝萧明凰赐死后穿越现代；核心竞争力不是现代学历，而是古代乱世炼出的识人术、纵横术、朝堂辩论、帝王心术、赈灾治国经验。所有角色均为虚构成年人；非照片；禁止写实真人脸、真实政治人物、文字/字幕/LOGO/水印。",
			}),
			Outputs: []Port{{ID: "text", Type: PortText}},
		},
		{
			ID: "asset_outline", Type: NodeStoryBrief, Position: Position{X: 300, Y: -170}, PositionMode: PositionAuto,
			Config: config(map[string]any{
				"title":  "01 大纲资产 / 30秒狗血节拍",
				"prompt": outlinePrompt,
				"episode_outline": []map[string]any{
					{"time": "0-7.5秒", "title": "毒酒闪回落地纽港", "beat": "被女帝赐死的谢无咎跌落寰联门口，被米国网红嘲笑，睁眼听见危机警报"},
					{"time": "7.5-15秒", "title": "救助站受辱夜学", "beat": "他被当黑户推开，转身在地下图书馆把现代国际法拆成合纵连横图"},
					{"time": "15-22.5秒", "title": "危机会议识人反杀", "beat": "亚伦拍桌羞辱，他用座次、手势、停顿点破各国底牌"},
					{"time": "22.5-30秒", "title": "主席台金句重逢", "beat": "他走向主席台打出金句，萧明凰在人群阴影里红眼出现"},
				},
			}),
			Inputs:  []Port{{ID: "brief", Type: PortText, Required: true}},
			Outputs: []Port{{ID: "text", Type: PortText}},
		},
		{
			ID: "asset_characters", Type: NodeStoryBrief, Position: Position{X: 300, Y: -20}, PositionMode: PositionAuto,
			Config: config(map[string]any{
				"title":  "02 角色生成资产 / 四人定妆",
				"prompt": characterPlanPrompt,
				"character_generation": []map[string]any{
					{"node_id": "role_hero", "name": "谢无咎", "visual": "旧黑朝服、血痕、束冠长发、冷静压迫眼神", "competency": []string{"识人术", "纵横术", "朝堂辩论", "乱世治理"}},
					{"node_id": "role_heroine", "name": "萧明凰", "visual": "玄金凤纹长袍、现代黑大衣、金簪发髻、高傲悔意", "function": "结尾追悔火葬场钩子"},
					{"node_id": "role_cousin", "name": "林晚棠", "visual": "短发、深蓝西装、工作证、平板", "function": "现代规则翻译者"},
					{"node_id": "role_rival", "name": "亚伦·霍克", "visual": "银灰西装、鹰形领针、冷笑强势坐姿", "function": "米国代表反派"},
				},
			}),
			Inputs:  []Port{{ID: "brief", Type: PortText, Required: true}},
			Outputs: []Port{{ID: "text", Type: PortText}},
		},
		{
			ID: "asset_world", Type: NodeStoryBrief, Position: Position{X: 300, Y: 130}, PositionMode: PositionAuto,
			Config: config(map[string]any{
				"title":  "03 世界观与剧情背景资产",
				"prompt": worldPrompt,
				"background_assets": []map[string]any{
					{"node_id": "background_1", "name": "纽港寰联总部外广场", "elements": []string{"玻璃幕墙", "万国旗", "警戒栏", "路面积水", "直播光点"}},
					{"node_id": "background_2", "name": "救助站/地下公共图书馆", "elements": []string{"旧长椅", "自动售货机冷光", "国际法书", "世界地图"}},
					{"node_id": "background_3", "name": "寰球联合会危机会议厅", "elements": []string{"环形会议桌", "同传耳机", "蓝色电子席牌", "世界地图屏"}},
					{"node_id": "background_4", "name": "主席台", "elements": []string{"弧形万国旗", "中央演讲台", "聚光灯", "记者席闪光"}},
				},
			}),
			Inputs:  []Port{{ID: "brief", Type: PortText, Required: true}},
			Outputs: []Port{{ID: "text", Type: PortText}},
		},
		{
			ID: "asset_director", Type: NodeStoryBrief, Position: Position{X: 300, Y: 280}, PositionMode: PositionAuto,
			Config: config(map[string]any{
				"title":  "04 Seedance2导演审核资产",
				"prompt": directorPrompt,
				"audit_schema": map[string]any{
					"must_pass":      []string{"memory_point", "unexpectedness", "emotion", "narrative"},
					"must_use_terms": []string{"推镜头", "环绕拍摄", "希区柯克变焦", "快切", "闪回", "叠化", "电影感", "3D 国漫 CG", "冷色调", "高对比度"},
				},
			}),
			Inputs:  []Port{{ID: "brief", Type: PortText, Required: true}},
			Outputs: []Port{{ID: "text", Type: PortText}},
		},
		{
			ID: "role_hero", Type: NodeCharacter, RoleID: "hero", Position: Position{X: 580, Y: 20}, PositionMode: PositionAuto,
			Config: config(map[string]any{"title": "角色图：谢无咎 / 古代权臣", "name": "谢无咎", "adult_age": 30, "candidate_count": 2, "view": "全身三视图 + 脸部特写 + 服装物料细节", "prompt": "读取上游角色生成资产。电影感、3D 国漫 CG 角色设定；虚构成年人男主谢无咎（30岁），大雍权臣穿越到现代，剑眉深目，长发束冠但外披旧黑色朝服，胸口有暗色血痕，眼神冷静压迫，像从朝堂与战场里活下来的人；核心竞争力是识人术、纵横术、朝堂辩论和乱世治理经验。" + characterSheetSuffix}),
			Inputs: characterInputs, Outputs: characterOutputs,
		},
		{
			ID: "role_heroine", Type: NodeCharacter, RoleID: "heroine", Position: Position{X: 580, Y: 170}, PositionMode: PositionAuto,
			Config: config(map[string]any{"title": "角色图：萧明凰 / 追悔女帝", "name": "萧明凰", "adult_age": 28, "candidate_count": 2, "view": "全身三视图 + 脸部特写 + 服装物料细节", "prompt": "读取上游角色生成资产。电影感、3D 国漫 CG 角色设定；虚构成年人女帝萧明凰（28岁），玄金凤纹长袍与现代黑色大衣混搭，金簪发髻，眼神高傲又带悔意；她曾赐死谢无咎，现代在主席台阴影里重逢，形成狗血追悔爆点。" + characterSheetSuffix}),
			Inputs: characterInputs, Outputs: characterOutputs,
		},
		{
			ID: "role_cousin", Type: NodeCharacter, RoleID: "cousin", Position: Position{X: 580, Y: 320}, PositionMode: PositionAuto,
			Config: config(map[string]any{"title": "角色图：林晚棠 / 现代外交官", "name": "林晚棠", "adult_age": 27, "candidate_count": 2, "view": "全身三视图 + 脸部特写 + 服装物料细节", "prompt": "读取上游角色生成资产。电影感、3D 国漫 CG 角色设定；虚构成年人华国青年外交官林晚棠（27岁），利落短发，白衬衫、深蓝西装、胸前工作证，手持平板和文件夹，眼神清醒坚定；她发现谢无咎把现代冲突画成古代合纵图，是现代规则与古代纵横术的桥梁。" + characterSheetSuffix}),
			Inputs: characterInputs, Outputs: characterOutputs,
		},
		{
			ID: "role_rival", Type: NodeCharacter, RoleID: "rival", Position: Position{X: 580, Y: 470}, PositionMode: PositionAuto,
			Config: config(map[string]any{"title": "角色图：亚伦·霍克 / 米国代表", "name": "亚伦·霍克", "adult_age": 42, "candidate_count": 2, "view": "全身三视图 + 脸部特写 + 服装物料细节", "prompt": "读取上游角色生成资产。电影感、3D 国漫 CG 角色设定；虚构成年人米国代表亚伦·霍克（42岁），银灰西装、鹰形领针、冷笑表情，坐姿强势，眼神精明多疑；他先嘲笑谢无咎是古装疯子，随后在危机会议现场被看穿选票、能源和军工订单底牌。" + characterSheetSuffix}),
			Inputs: characterInputs, Outputs: characterOutputs,
		},
		{
			ID: "script", Type: NodeScript, Position: Position{X: 860, Y: 245}, PositionMode: PositionAuto,
			Config: config(map[string]any{"title": "05 剧本生成 / 创意审核后输出JSON", "scene_count": 4, "prompt": scriptPrompt}),
			Inputs: scriptInputs, Outputs: []Port{{ID: "script", Type: PortScript}},
		},
	}
	sceneTitles := []string{"毒酒闪回落地纽港", "救助站受辱夜学", "危机会议识人反杀", "主席台金句重逢"}
	backgrounds := []string{
		"纽港雨夜，寰球联合会总部外广场，玻璃幕墙、万国旗、警戒栏、路面积水反光、手机直播光点",
		"纽港救助站与地下公共图书馆之间的深夜走廊，旧长椅、自动售货机冷光、国际法书堆、世界地图",
		"寰球联合会危机会议厅，环形会议桌、同声传译耳机、蓝色电子席牌、巨幅世界地图屏幕、冷白顶光",
		"寰球联合会主席台，弧形万国旗、中央演讲台、聚光灯、玻璃穹顶、远处记者席闪光",
	}
	videoBeats := []string{
		"毒酒入喉的闪回叠化到纽港雨夜，谢无咎穿染血朝服跌落在寰球联合会门口，被米国网红嘲笑后猛然睁眼",
		"救助站工作人员把谢无咎推开，他转入地下图书馆快切夜学，林晚棠看到他把冲突国利益线画成合纵图",
		"亚伦·霍克在危机会议厅拍桌嘲讽，谢无咎用希区柯克变焦和环绕拍摄压住全场，点破各方真实底牌",
		"主席台聚光灯亮起，谢无咎走向演讲台打出金句，萧明凰在阴影中红着眼出现，旧日赐死者重逢",
	}
	for i := 1; i <= 4; i++ {
		y := float64(20 + (i-1)*200)
		sceneID := fmt.Sprintf("scene_%d", i)
		backgroundID := fmt.Sprintf("background_%d", i)
		videoID := fmt.Sprintf("video_%d", i)
		graph.Nodes = append(graph.Nodes,
			Node{
				ID: sceneID, Type: NodeScene, SceneID: sceneID, DurationSeconds: SceneDuration,
				Position: Position{X: 860, Y: y}, PositionMode: PositionAuto,
				Config: config(map[string]any{"index": i, "title": sceneTitles[i-1]}),
				Inputs: []Port{{ID: "script", Type: PortScript, Required: true}}, Outputs: []Port{{ID: "scene", Type: PortScene}},
			},
			Node{
				ID: backgroundID, Type: NodeBackground, SceneID: sceneID,
				Position: Position{X: 1080, Y: y}, PositionMode: PositionAuto,
				Config: config(map[string]any{"title": fmt.Sprintf("背景图：%s", sceneTitles[i-1]), "candidate_count": 1, "prompt": backgroundPrompt(sceneTitles[i-1], backgrounds[i-1])}),
				Inputs: []Port{
					{ID: "environment", Label: backgroundEnvironmentPortLabel, Type: PortScene, Required: true},
					{ID: "world", Label: "世界观/背景资产", Type: PortText, Required: true},
				}, Outputs: []Port{{ID: "image", Type: PortImage}},
			},
			Node{
				ID: videoID, Type: NodeVideo, SceneID: sceneID, DurationSeconds: SceneDuration,
				Position: Position{X: 1300, Y: y}, PositionMode: PositionAuto,
				Config: config(map[string]any{"title": fmt.Sprintf("视频%d：%s", i, sceneTitles[i-1]), "duration_seconds": SceneDuration, "prompt": videoPrompt(i, sceneTitles[i-1], videoBeats[i-1])}),
				Inputs: videoInputs, Outputs: []Port{{ID: "video", Type: PortVideo}},
			},
		)
	}
	graph.Nodes = append(graph.Nodes,
		Node{
			ID: "timeline", Type: NodeTimeline, Locked: true, Position: Position{X: 1560, Y: 320}, PositionMode: PositionAuto,
			Config: timelineConfig,
			Inputs: []Port{
				{ID: "clip_1", Type: PortVideo, Required: true},
				{ID: "clip_2", Type: PortVideo, Required: true},
				{ID: "clip_3", Type: PortVideo, Required: true},
				{ID: "clip_4", Type: PortVideo, Required: true},
			},
			Outputs: []Port{{ID: "videos", Type: PortVideoList}},
		},
		Node{
			ID: "compose", Type: NodeCompose, Locked: true, Position: Position{X: 1800, Y: 320}, PositionMode: PositionAuto,
			Inputs: []Port{{ID: "videos", Type: PortVideoList, Required: true}}, Outputs: []Port{{ID: "video", Type: PortVideo}},
		},
	)
	graph.Edges = []Edge{
		{ID: "e_brief_asset_outline", Source: "brief", SourcePort: "text", Target: "asset_outline", TargetPort: "brief"},
		{ID: "e_brief_asset_characters", Source: "brief", SourcePort: "text", Target: "asset_characters", TargetPort: "brief"},
		{ID: "e_brief_asset_world", Source: "brief", SourcePort: "text", Target: "asset_world", TargetPort: "brief"},
		{ID: "e_brief_asset_director", Source: "brief", SourcePort: "text", Target: "asset_director", TargetPort: "brief"},
		{ID: "e_brief_hero", Source: "brief", SourcePort: "text", Target: "role_hero", TargetPort: "brief"},
		{ID: "e_brief_heroine", Source: "brief", SourcePort: "text", Target: "role_heroine", TargetPort: "brief"},
		{ID: "e_brief_cousin", Source: "brief", SourcePort: "text", Target: "role_cousin", TargetPort: "brief"},
		{ID: "e_brief_rival", Source: "brief", SourcePort: "text", Target: "role_rival", TargetPort: "brief"},
		{ID: "e_brief_script", Source: "brief", SourcePort: "text", Target: "script", TargetPort: "brief"},
		{ID: "e_asset_outline_script", Source: "asset_outline", SourcePort: "text", Target: "script", TargetPort: "outline"},
		{ID: "e_asset_characters_script", Source: "asset_characters", SourcePort: "text", Target: "script", TargetPort: "character_plan"},
		{ID: "e_asset_world_script", Source: "asset_world", SourcePort: "text", Target: "script", TargetPort: "world"},
		{ID: "e_asset_director_script", Source: "asset_director", SourcePort: "text", Target: "script", TargetPort: "director"},
		{ID: "e_asset_characters_hero", Source: "asset_characters", SourcePort: "text", Target: "role_hero", TargetPort: "character_plan"},
		{ID: "e_asset_characters_heroine", Source: "asset_characters", SourcePort: "text", Target: "role_heroine", TargetPort: "character_plan"},
		{ID: "e_asset_characters_cousin", Source: "asset_characters", SourcePort: "text", Target: "role_cousin", TargetPort: "character_plan"},
		{ID: "e_asset_characters_rival", Source: "asset_characters", SourcePort: "text", Target: "role_rival", TargetPort: "character_plan"},
		{ID: "e_hero_script", Source: "role_hero", SourcePort: "selected", Target: "script", TargetPort: "hero"},
		{ID: "e_heroine_script", Source: "role_heroine", SourcePort: "selected", Target: "script", TargetPort: "heroine"},
		{ID: "e_cousin_script", Source: "role_cousin", SourcePort: "selected", Target: "script", TargetPort: "cousin"},
		{ID: "e_rival_script", Source: "role_rival", SourcePort: "selected", Target: "script", TargetPort: "rival"},
		{ID: "e_timeline_compose", Source: "timeline", SourcePort: "videos", Target: "compose", TargetPort: "videos"},
	}
	for i := 1; i <= 4; i++ {
		sceneID := fmt.Sprintf("scene_%d", i)
		backgroundID := fmt.Sprintf("background_%d", i)
		videoID := fmt.Sprintf("video_%d", i)
		graph.Edges = append(graph.Edges,
			Edge{ID: fmt.Sprintf("e_script_scene_%d", i), Source: "script", SourcePort: "script", Target: sceneID, TargetPort: "script"},
			Edge{ID: fmt.Sprintf("e_scene_background_%d", i), Source: sceneID, SourcePort: "scene", Target: backgroundID, TargetPort: "environment"},
			Edge{ID: fmt.Sprintf("e_asset_world_background_%d", i), Source: "asset_world", SourcePort: "text", Target: backgroundID, TargetPort: "world"},
			Edge{ID: fmt.Sprintf("e_scene_video_%d", i), Source: sceneID, SourcePort: "scene", Target: videoID, TargetPort: "scene"},
			Edge{ID: fmt.Sprintf("e_asset_director_video_%d", i), Source: "asset_director", SourcePort: "text", Target: videoID, TargetPort: "director"},
			Edge{ID: fmt.Sprintf("e_background_video_%d", i), Source: backgroundID, SourcePort: "image", Target: videoID, TargetPort: "background"},
			Edge{ID: fmt.Sprintf("e_hero_video_%d", i), Source: "role_hero", SourcePort: "selected", Target: videoID, TargetPort: "hero"},
			Edge{ID: fmt.Sprintf("e_heroine_video_%d", i), Source: "role_heroine", SourcePort: "selected", Target: videoID, TargetPort: "heroine"},
			Edge{ID: fmt.Sprintf("e_cousin_video_%d", i), Source: "role_cousin", SourcePort: "selected", Target: videoID, TargetPort: "cousin"},
			Edge{ID: fmt.Sprintf("e_rival_video_%d", i), Source: "role_rival", SourcePort: "selected", Target: videoID, TargetPort: "rival"},
			Edge{ID: fmt.Sprintf("e_video_timeline_%d", i), Source: videoID, SourcePort: "video", Target: "timeline", TargetPort: fmt.Sprintf("clip_%d", i)},
		)
		graph.Groups = append(graph.Groups, Group{
			ID: fmt.Sprintf("scene_group_%d", i), Type: "scene", SceneID: sceneID, Enabled: true, DurationSeconds: SceneDuration,
			NodeIDs: []string{sceneID, backgroundID, videoID}, Position: Position{X: 840, Y: float64(0 + (i-1)*200)}, Size: Size{Width: 680, Height: 170},
		})
	}
	if configErr != nil {
		return Template{}, configErr
	}
	NormalizeBackgroundEnvironmentPorts(&graph)
	template := Template{
		Code: "global_chairman_drama_seedance", Version: 2, Name: "古人穿越主席短剧 · Seedance2 资产拆解版",
		Description: "近现实架空分集短剧工作流：画布显式拆出大纲资产、角色生成资产、世界观/剧情背景资产和 Seedance2 导演审核资产，再生成4张角色参考图、4张场景首帧、4段15秒 Seedance 视频并裁剪合成约30秒单集。",
		Enabled:     true, Graph: graph,
	}
	briefIndex := findTemplateNodeIndex(template.Graph, "brief")
	if briefIndex < 0 {
		return Template{}, fmt.Errorf("global chairman brief node not found")
	}
	if err := applyGlobalChairmanBriefAssetPackage(&template.Graph.Nodes[briefIndex]); err != nil {
		return Template{}, err
	}
	return template, nil
}

func applyAncientDramaV4Defaults(graph *Graph) error {
	const characterSheetSuffix = "读取上游人物小传资产，输出一张角色设定参考图：上排全身正面、侧面、背面三视图；中排脸部大特写（正面与3/4侧）刻画五官、妆容、眼神与发型发丝细节；下排服装与物料细节图（面料纹理、刺绣纹样、扣襟缘饰、配饰、鞋履、腰带/披帛等材质特写）；浅底设定稿、布局清晰；统一面容、发型、服装和气质；这是角色设定参考图，不是剧情镜头；非照片、非写实、非真人。"
	characterPrompts := map[string]string{
		"role_heroine": "二维国风动画角色设定；虚构成年人古风女主（22岁），鹅蛋脸杏眼，乌黑高挽发髻插白玉簪，穿月白交领襦裙配浅青披帛，气质清冷克制。" + characterSheetSuffix,
		"role_hero":    "二维国风动画角色设定；虚构成年人古风男主（25岁），剑眉星目，束发玉冠，青色圆领袍外罩玄色披风，腰佩木剑，气质沉稳。" + characterSheetSuffix,
		"role_cousin":  "二维国风动画角色设定；虚构成年人古风表小姐（23岁），桃花妆圆脸，双环髻缀红花，朱红罗裙绣金纹，笑意明亮。" + characterSheetSuffix,
	}
	characterViews := map[string]string{
		"role_heroine": "全身三视图 + 脸部特写 + 服装物料细节",
		"role_hero":    "全身三视图 + 脸部特写 + 服装物料细节",
		"role_cousin":  "全身三视图 + 脸部特写 + 服装物料细节",
	}
	backgroundPrompts := map[string]string{
		"background_1": "读取上游场景设定表，仅提取地点、时间、光影、空间布局、道具和氛围。二维国风动画场景背景；江南雨夜青石板巷，纸伞与灯笼倒影，远处木门半掩；无人或仅远景剪影；虚构成年人世界观；非照片、非写实、非真人；9:16单镜头、非拼贴，保持统一古风美术与清晰空间层次。",
		"background_2": "读取上游场景设定表，仅提取地点、时间、光影、空间布局、道具和氛围。二维国风动画场景背景；府邸后花园夜色，石桥、垂柳、凉亭烛火，池面映星；无人空镜；虚构成年人世界观；非照片、非写实、非真人；9:16单镜头、非拼贴，保持统一古风美术与清晰空间层次。",
		"background_3": "读取上游场景设定表，仅提取地点、时间、光影、空间布局、道具和氛围。二维国风动画场景背景；朱漆宴厅华灯高悬，红烛长案、屏风字画，热闹却空无一人；虚构成年人世界观；非照片、非写实、非真人；9:16单镜头、非拼贴，保持统一古风美术与清晰空间层次。",
		"background_4": "读取上游场景设定表，仅提取地点、时间、光影、空间布局、道具和氛围。二维国风动画场景背景；河岸放河灯，水面漂满莲花灯，远山淡墨月色；空镜无特写人物；虚构成年人世界观；非照片、非写实、非真人；9:16单镜头、非拼贴，保持统一古风美术与清晰空间层次。",
	}
	videoPrompts := map[string]string{
		"video_1": "9:16，30fps，15秒，二维国风动画短剧。参考图用途：@图片1为场景背景参考，@图片2为女主角色参考，@图片3为男主角色参考，@图片4为表小姐角色参考。根据上游分镜镜头表生成第1幕“雨巷重逢”：0-3秒近景推镜头，雨滴落在纸伞边缘；4-8秒中景跟拍，女主撑伞穿过青石巷；9-12秒特写焦点转移，男主停步抬眼；13-15秒拉镜头，两人在灯笼倒影中对视。台词用“角色（情绪）：台词”；音效包含环境音、雨声、衣料摩擦。所有角色均为虚构成年人；非照片、非写实、非真人；保持角色造型、背景美术和动作连续一致。禁止：字幕、LOGO、水印、写实真人脸、角色换脸、参考图拼贴布局入镜。",
		"video_2": "9:16，30fps，15秒，二维国风动画短剧。参考图用途：@图片1为场景背景参考，@图片2为女主角色参考，@图片3为男主角色参考，@图片4为表小姐角色参考。根据上游分镜镜头表生成第2幕“花园叙旧”：0-3秒全景拉镜头展示凉亭与池面星光；4-8秒中景移镜头，三人围坐寒暄；9-12秒近景焦点转移，表小姐打趣时女主神色一沉；13-15秒特写，男主察觉旧约话题被触动。台词用“角色（情绪）：台词”；音效包含环境音、轻柔古筝、风过柳叶。所有角色均为虚构成年人；非照片、非写实、非真人；保持角色造型、背景美术和动作连续一致。禁止：字幕、LOGO、水印、写实真人脸、角色换脸、参考图拼贴布局入镜。",
		"video_3": "9:16，30fps，15秒，二维国风动画短剧。参考图用途：@图片1为场景背景参考，@图片2为女主角色参考，@图片3为男主角色参考，@图片4为表小姐角色参考。根据上游分镜镜头表生成第3幕“宴厅冲突”：0-3秒对称构图全景，红烛与屏风压出紧张气氛；4-8秒手持跟拍，旧婚约被提起，众人目光汇聚；9-12秒特写，女主压住情绪抽身离席；13-15秒中景跟拍，男主追出宴厅。台词用“角色（情绪）：台词”；音效包含杯盏轻响、短促鼓点、环境音骤弱。所有角色均为虚构成年人；非照片、非写实、非真人；保持角色造型、背景美术和动作连续一致。禁止：字幕、LOGO、水印、写实真人脸、角色换脸、参考图拼贴布局入镜。",
		"video_4": "9:16，30fps，15秒，二维国风动画短剧。参考图用途：@图片1为场景背景参考，@图片2为女主角色参考，@图片3为男主角色参考，@图片4为表小姐角色参考。根据上游分镜镜头表生成第4幕“河灯和解”：0-3秒大远景拉镜头，河灯铺满水面；4-8秒近景推镜头，两人并肩放下河灯；9-12秒特写焦点转移，旧误会在低声对白中松开；13-15秒淡入淡出，表小姐远处祝福，河灯顺水远去。台词用“角色（情绪）：台词”；音效包含水声、低声对白、音乐渐暖。所有角色均为虚构成年人；非照片、非写实、非真人；保持角色造型、背景美术和动作连续一致。禁止：字幕、LOGO、水印、写实真人脸、角色换脸、参考图拼贴布局入镜。",
	}
	for i := range graph.Nodes {
		node := &graph.Nodes[i]
		var prompt string
		switch {
		case node.ID == "brief":
			prompt = "创作四幕古风短剧《雨巷旧约》的前期材料包：故事圣经、人物小传、场景设定表、分集剧情路线、分镜镜头规范和 Seedance 提示词契约。故事：女主离乡三年雨夜归来，重逢青梅竹马男主与热情表小姐；冲突是当年未解的婚约与误会，结局河灯夜和解。视觉统一为二维国风动画，所有角色均为虚构成年人；非照片、非写实、非真人。四幕分别为：雨巷重逢、花园叙旧、宴厅冲突、河灯和解。"
			if err := applyAncientDramaBriefAssetPackage(node); err != nil {
				return err
			}
		case node.ID == "script" || node.Type == NodeScript:
			prompt = "基于上游故事圣经、人物小传、场景设定表和分集剧情路线，输出严格 JSON，不要 Markdown。顶层字段必须包含 project、characters、locations、episodes、scenes、seedance_prompt_contract。characters 必须包含 heroine、hero、cousin 三个角色的人物小传、关系、动机、性格、服装妆发和表演方式。locations 必须包含 rain_alley、garden、banquet_hall、river_lantern 四个场景的空间布局、光影、道具和禁用项。episodes 必须包含四幕剧情目标、冲突推进、转折和结尾钩子。scenes 必须恰好 4 项，每项固定 15 秒，并包含 id、episode_id、title、duration_seconds、location_id、location、dramatic_beat、conflict、emotional_turn、dialogues、shots、audio。每个 shots 必须覆盖 0-3秒、4-8秒、9-12秒、13-15秒，且每个镜头包含 shot_id、time_range、景别、运镜、构图、action、expression、lighting、dialogue、audio、seedance_prompt。台词必须用角色名和情绪标注，例如“女主（克制）：我不是回来认输的。”；镜头词只使用景别、推镜头、拉镜头、移镜头、跟拍、特写、焦点转移、淡入淡出、环境音、音桥等规范词。所有角色均为虚构成年人；视觉为二维国风动画；禁止照片写实、真人脸、字幕、LOGO、水印。"
		case characterPrompts[node.ID] != "":
			prompt = characterPrompts[node.ID]
			if view := characterViews[node.ID]; view != "" {
				if err := setTemplateNodeStringField(node, "view", view); err != nil {
					return fmt.Errorf("set ancient drama v4 view for %s: %w", node.ID, err)
				}
			}
		case backgroundPrompts[node.ID] != "":
			prompt = backgroundPrompts[node.ID]
		case node.Type == NodeBackground:
			prompt = "读取上游场景设定表，仅提取地点、时间、光影、空间布局、道具和氛围。生成二维国风动画场景背景；无人空镜或仅远景不可辨识剪影；禁止近中景人物与肢体动作；道具仅可静置；非照片、非写实、非真人；9:16单镜头、非拼贴，保持统一古风美术与清晰空间层次。"
		case videoPrompts[node.ID] != "":
			prompt = videoPrompts[node.ID]
		case node.Type == NodeVideo:
			prompt = "9:16，30fps，15秒，二维国风动画短剧。参考图用途：@图片1为场景背景参考，@图片2为女主角色参考，@图片3为男主角色参考，@图片4为表小姐角色参考。请读取上游分镜镜头表，把 shots 转成 0-3秒、4-8秒、9-12秒、13-15秒的 Seedance 提示词；台词用“角色（情绪）：台词”；音效写环境音、音桥或音乐情绪。所有角色均为虚构成年人；非照片、非写实、非真人；保持角色造型、背景美术和动作连续一致。禁止：字幕、LOGO、水印、写实真人脸、角色换脸、参考图拼贴布局入镜。"
		case node.Type == NodeTimeline:
			var config TimelineConfig
			if err := json.Unmarshal(node.Config, &config); err != nil {
				return fmt.Errorf("decode ancient drama v4 timeline: %w", err)
			}
			if len(config.Clips) != 4 {
				return fmt.Errorf("ancient drama v4 timeline has %d clips, want 4", len(config.Clips))
			}
			config.Clips[1].TrimInMS = 1_200
			config.Clips[1].TrimOutMS = 12_000
			encodedConfig, err := json.Marshal(config)
			if err != nil {
				return err
			}
			node.Config = encodedConfig
		}
		if prompt != "" {
			if err := setTemplateNodePrompt(node, prompt); err != nil {
				return fmt.Errorf("set ancient drama v4 prompt for %s: %w", node.ID, err)
			}
		}
	}
	return nil
}

func applyAncientDramaBriefAssetPackage(node *Node) error {
	fields := map[string]any{
		"series_bible": map[string]any{
			"title":        "雨巷旧约",
			"format":       "四幕古风竖屏短剧",
			"theme":        "旧约、误会、归来与和解",
			"style":        "二维国风动画；水墨光影；低饱和月色与灯笼暖光对比",
			"memory_point": "雨夜纸伞重逢与河灯夜和解形成首尾呼应",
			"emotion_arc":  []string{"雨夜压抑", "花园试探", "宴厅爆发", "河灯释然"},
			"safety":       []string{"虚构成年人", "非照片", "非写实", "非真人", "禁止字幕/LOGO/水印"},
		},
		"character_bible": []map[string]any{
			{
				"id": "heroine", "node_id": "role_heroine", "name": "沈青辞", "age": 22, "role": "女主",
				"motivation": "查清三年前离乡与旧婚约误会", "personality": "清冷克制，情绪压在眼神和停顿里",
				"relationship": "与男主青梅竹马；与表小姐既亲近又被旧事牵动",
				"appearance":   "鹅蛋脸杏眼，乌黑高挽发髻插白玉簪，月白交领襦裙配浅青披帛",
				"performance":  "少大幅表演，更多用回眸、停步、低声对白表达情绪",
			},
			{
				"id": "hero", "node_id": "role_hero", "name": "陆知衡", "age": 25, "role": "男主",
				"motivation": "追回三年前没有说出口的真相", "personality": "沉稳内敛，关键时刻主动追问",
				"relationship": "女主青梅竹马；被旧婚约牵制却想主动解释",
				"appearance":   "剑眉星目，束发玉冠，青色圆领袍外罩玄色披风，腰佩木剑",
				"performance":  "站姿克制，情绪转折落在抬眼、追步和伸手停顿",
			},
			{
				"id": "cousin", "node_id": "role_cousin", "name": "苏云萝", "age": 23, "role": "表小姐",
				"motivation": "用玩笑维持表面热闹，也推动旧约浮出水面", "personality": "明亮外放，话锋里藏着试探",
				"relationship": "女主表亲，男主旧约风波的旁观者与推动者",
				"appearance":   "桃花妆圆脸，双环髻缀红花，朱红罗裙绣金纹",
				"performance":  "轻快动作与忽然收住的笑形成反差",
			},
		},
		"scene_bible": []map[string]any{
			{"id": "rain_alley", "node_id": "background_1", "name": "江南雨夜青石巷", "time": "雨夜", "lighting": "灯笼暖光与青石倒影", "props": []string{"纸伞", "灯笼", "半掩木门"}, "rule": "无人空镜或远景剪影"},
			{"id": "garden", "node_id": "background_2", "name": "府邸后花园凉亭", "time": "夜色", "lighting": "烛火、星光和池面反光", "props": []string{"石桥", "垂柳", "凉亭", "池水"}, "rule": "无人空镜"},
			{"id": "banquet_hall", "node_id": "background_3", "name": "朱漆宴厅", "time": "夜宴", "lighting": "红烛、华灯和屏风阴影", "props": []string{"长案", "酒盏", "屏风", "字画"}, "rule": "空场景，不出现可辨识人物"},
			{"id": "river_lantern", "node_id": "background_4", "name": "河岸放河灯", "time": "月夜", "lighting": "月色、水面河灯暖光", "props": []string{"莲花灯", "远山", "水纹"}, "rule": "空镜无特写人物"},
		},
		"episode_route": []map[string]any{
			{"episode_id": "ep_1", "scene_id": "scene_1", "title": "雨巷重逢", "goal": "让女主归来与男主停步对视", "conflict": "三年前旧约未解", "hook": "两人都认出对方却没有先解释"},
			{"episode_id": "ep_2", "scene_id": "scene_2", "title": "花园叙旧", "goal": "用表小姐玩笑把旧事推到台面", "conflict": "热闹表象下的试探", "hook": "女主听到旧约后笑意消失"},
			{"episode_id": "ep_3", "scene_id": "scene_3", "title": "宴厅冲突", "goal": "公开触发误会爆点", "conflict": "旧婚约被当众提起", "hook": "女主离席，男主追出"},
			{"episode_id": "ep_4", "scene_id": "scene_4", "title": "河灯和解", "goal": "两人放下误会并形成首尾呼应", "conflict": "是否还相信彼此", "hook": "河灯远去，旧约不再束缚两人"},
		},
		"storyboard_contract": map[string]any{
			"scene_count":           4,
			"duration_seconds_each": 15,
			"time_ranges":           []string{"0-3秒", "4-8秒", "9-12秒", "13-15秒"},
			"shot_fields":           []string{"shot_id", "time_range", "景别", "运镜", "构图", "action", "expression", "lighting", "dialogue", "audio", "seedance_prompt"},
			"allowed_camera_terms":  []string{"远景", "全景", "中景", "近景", "特写", "大特写", "推镜头", "拉镜头", "移镜头", "跟拍", "焦点转移", "淡入淡出"},
		},
		"seedance_contract": map[string]any{
			"image_references": []string{"@图片1为场景背景参考", "@图片2为女主角色参考", "@图片3为男主角色参考", "@图片4为表小姐角色参考"},
			"prompt_language":  "中文",
			"video_prefix":     "9:16，30fps，15秒，二维国风动画短剧",
			"must_include":     []string{"时间戳分镜", "台词情绪标注", "环境音或音桥", "禁止字幕/LOGO/水印"},
		},
	}
	for key, value := range fields {
		if err := setTemplateNodeJSONField(node, key, value); err != nil {
			return fmt.Errorf("set ancient drama v4 %s: %w", key, err)
		}
	}
	return nil
}

func applyGlobalChairmanBriefAssetPackage(node *Node) error {
	fields := map[string]any{
		"source_materials": map[string]any{
			"user_intent":       "分集短剧素材，9:16，每条约30秒；故事要狗血、短剧化，并让古人的技能成为现代核心竞争力。",
			"series_title":      "千年权臣：我在纽港执掌寰球联合会",
			"episode_title":     "第1集 古装疯子闯寰联",
			"source_hook":       "男主被女帝赐毒酒后穿越到纽港寰球联合会门口，被现代人嘲笑，却在危机会议中用古代朝堂能力完成反杀。",
			"aspect_ratio":      "9:16",
			"final_duration_ms": 30000,
			"generation_plan":   "4段15秒 Seedance 视频，每段前7.5秒完成有效剧情，时间线各裁0-7500ms合成30秒。",
			"safety":            []string{"虚构成年人", "近现实架空", "非照片", "禁止写实真人脸", "禁止真实政治人物", "禁止文字/字幕/LOGO/水印"},
		},
		"seedance2_workflow": []string{
			"素材/题材诊断：识别用户输入里的题材、核心爽点、风险和缺失信息",
			"创意发散：至少生成2-3个完全不同方向，不直接套固定模板",
			"方向选择：选择最有记忆点、意外感、情绪和叙事的一版",
			"文案扩写：把方向扩成可直接生成的中文 Seedance prompt",
			"运镜匹配：只从 reference.md 词库中选运镜、节奏、风格词",
			"搭配验证：检查角色参考图、场景首帧、prompt、运镜是否协调",
			"创意审核：记忆点、意外感、情绪、叙事四项必须通过后再进入视频节点",
		},
		"creative_direction_candidates": []map[string]any{
			{
				"id": "A", "name": "古装疯子当场封神",
				"logline":         "人人嘲笑他不懂现代文明，他却像审早朝一样读出每个代表的真实底牌。",
				"memory_point":    "用古代识人术破解现代危机会议",
				"unexpected":      "古代权臣不靠手机和学历，而靠停顿、座次、手势判断局势",
				"emotion_arc":     []string{"濒死", "被辱", "冷醒", "反杀"},
				"recommended_use": "第1集主线，最适合30秒短剧强钩子",
			},
			{
				"id": "B", "name": "女帝追悔火葬场",
				"logline":         "她曾怕他权倾朝野，如今看见他站上寰球联合会主席台。",
				"memory_point":    "旧日赐死与现代登顶形成狗血反差",
				"unexpected":      "女帝不是立刻相认，而是在人群阴影里亲眼看见他被万国注视",
				"emotion_arc":     []string{"狠心", "错失", "震惊", "追悔"},
				"recommended_use": "第1集结尾钩子",
			},
			{
				"id": "C", "name": "乱世治理降维打击",
				"logline":         "现代专家争论停战条款，他把赈灾、粮道、盟约和人心拆成一张古代战时治理图。",
				"memory_point":    "古代乱世治理经验变成现代国际方案",
				"unexpected":      "不是玄学开挂，而是古代制度经验迁移到现代危机处理",
				"emotion_arc":     []string{"混乱", "识破", "定策", "敬畏"},
				"recommended_use": "后续升级集延展",
			},
		},
		"world_bible": map[string]any{
			"fictional_names": []string{"米国", "罗斯国", "东瀛国", "华国", "纽港", "鹰宫", "五环楼", "寰球联合会"},
			"naming_rule":     "地点、国家、机构与现实类似但必须架空；不得使用真实政治人物姓名、真实联合机构名称或真实会议事件。",
			"visual_style":    []string{"电影感", "3D 国漫 CG", "HDR", "冷色调", "高对比度", "胶片质感", "硬光剪影", "镜头光晕"},
		},
		"character_bible": []map[string]any{
			{
				"id": "hero", "node_id": "role_hero", "name": "谢无咎", "age": 30, "role": "古代权臣男主",
				"motivation": "在现代活下去，并证明权力可以用来止战而不是夺命", "core_skill": []string{"识人术", "纵横术", "朝堂辩论", "乱世治理", "帝王心术", "赈灾治国"},
				"relationship": "被女帝赐死；被林晚棠发现才能；被亚伦·霍克公开羞辱后反杀",
				"appearance":   "旧黑色朝服、暗色血痕、束冠长发、冷静压迫的眼神",
				"performance":  "很少激动，用停顿、扫视、低声判断制造压迫感",
			},
			{
				"id": "heroine", "node_id": "role_heroine", "name": "萧明凰", "age": 28, "role": "穿越女帝",
				"motivation": "从忌惮男主到后悔失去男主", "core_skill": []string{"帝王气场", "情绪反差", "旧爱追悔"},
				"relationship": "曾赐死谢无咎，现代在主席台阴影处重逢",
				"appearance":   "玄金凤纹长袍与现代黑色大衣混搭，金簪发髻",
				"performance":  "前期高傲冷硬，重逢时眼眶泛红但仍强撑体面",
			},
			{
				"id": "cousin", "node_id": "role_cousin", "name": "林晚棠", "age": 27, "role": "华国青年外交官",
				"motivation": "把男主从黑户和疯子标签里捞出来，验证他的古代纵横术", "core_skill": []string{"现代规则", "外交专业", "观察力"},
				"relationship": "是男主现代世界的发现者与临时担保人",
				"appearance":   "利落短发、深蓝西装、白衬衫、工作证和平板",
				"performance":  "从怀疑到震惊，情绪落在抬眼、停笔和快速记录",
			},
			{
				"id": "rival", "node_id": "role_rival", "name": "亚伦·霍克", "age": 42, "role": "米国反派代表",
				"motivation": "维持强势阵营的话语权，把危机谈判变成筹码", "core_skill": []string{"施压", "嘲讽", "掩盖底牌"},
				"relationship": "公开羞辱谢无咎，随后被谢无咎点破选票、能源和军工订单底牌",
				"appearance":   "银灰西装、鹰形领针、冷笑表情、强势坐姿",
				"performance":  "先拍桌压人，后表情僵硬、手指停止敲桌",
			},
		},
		"core_competency_matrix": []map[string]any{
			{"ancient_skill": "识人术", "modern_gap": "现代谈判依赖资料和简报，容易忽略人的微表情、座次、停顿和临场恐惧", "scene_use": "危机会议中看穿亚伦手指停顿、代表席位交换和发言顺序变化"},
			{"ancient_skill": "纵横术", "modern_gap": "现代代表各说各话，缺少把利益线快速重组的能力", "scene_use": "把罗斯国、东瀛国、米国、华国的诉求画成合纵图"},
			{"ancient_skill": "朝堂辩论", "modern_gap": "现代会议语言礼貌却含混，缺少一锤定音的公开反杀", "scene_use": "用一句“不是想停战，是想体面认输”压住全场"},
			{"ancient_skill": "乱世治理", "modern_gap": "现代危机处理分部门割裂，缺少战后粮道、赈济、人心安抚的一体化视角", "scene_use": "后续可升级为联合停火和救援方案"},
		},
		"scene_bible": []map[string]any{
			{"id": "arrival_plaza", "node_id": "background_1", "name": "纽港寰球联合会总部外广场", "time": "雨夜", "lighting": "冷蓝玻璃幕墙、手机直播灯、路面积水反光", "props": []string{"万国旗", "警戒栏", "直播屏幕", "手机灯"}, "rule": "无人空镜或远景剪影"},
			{"id": "night_study", "node_id": "background_2", "name": "救助站与地下公共图书馆走廊", "time": "深夜", "lighting": "荧光灯与自动售货机冷光", "props": []string{"旧长椅", "国际法书", "地图", "平板"}, "rule": "无人空镜"},
			{"id": "crisis_hall", "node_id": "background_3", "name": "寰球联合会危机会议厅", "time": "白天会议", "lighting": "冷白顶光与蓝色电子席牌", "props": []string{"环形会议桌", "同声传译耳机", "世界地图屏幕"}, "rule": "空场景，不出现可辨识人物"},
			{"id": "chair_podium", "node_id": "background_4", "name": "寰球联合会主席台", "time": "聚光灯时刻", "lighting": "主席台聚光灯、旗帜背光、记者席闪光", "props": []string{"中央演讲台", "万国旗", "玻璃穹顶"}, "rule": "空镜无特写人物"},
		},
		"scene_strategy": []map[string]any{
			{"scene_id": "scene_1", "title": "穿越落地", "goal": "用毒酒闪回和纽港雨夜建立强钩子", "conflict": "古代权臣被现代人当成古装疯子", "hook": "他睁眼时听见寰联警报"},
			{"scene_id": "scene_2", "title": "受辱夜学", "goal": "证明核心竞争力不是学历，而是把现代规则翻译成古代权力图谱", "conflict": "救助站羞辱与身份黑户", "hook": "林晚棠发现他的合纵图"},
			{"scene_id": "scene_3", "title": "会议反杀", "goal": "用识人术和纵横术打脸米国代表", "conflict": "亚伦嘲讽他不懂现代文明", "hook": "谢无咎点破各方不是想停战而是想体面下台"},
			{"scene_id": "scene_4", "title": "主席台悬念", "goal": "用金句完成爽点并埋女帝重逢", "conflict": "旧日赐死者亲眼看见他被万国注视", "hook": "萧明凰在人群阴影中出现"},
		},
		"storyboard_contract": map[string]any{
			"scene_count":            4,
			"duration_seconds_each":  15,
			"effective_trim_ms_each": 7500,
			"final_duration_ms":      30000,
			"time_ranges":            []string{"0-2秒", "2-5秒", "5-7.5秒", "7.5-15秒余韵"},
			"shot_fields":            []string{"index", "scene_id", "title", "location_id", "characters", "dramatic_beat", "ancient_competency", "shots", "dialogue", "audio", "image_prompt", "seedance_prompt"},
			"must_output_fields":     []string{"source_diagnosis", "creative_directions", "selected_direction", "reference_terms_used", "compatibility_check", "creative_audit", "scenes"},
		},
		"reference_terms_contract": map[string]any{
			"camera":    []string{"大远景", "全景", "中景", "近景", "特写", "推镜头", "拉镜头", "摇镜头", "移镜头", "跟拍", "环绕拍摄", "手持跟拍", "希区柯克变焦", "低角度", "仰拍", "焦点转移"},
			"rhythm":    []string{"快切", "硬切", "闪回", "叠化", "交叉蒙太奇", "平行蒙太奇", "音桥", "无声处理"},
			"style":     []string{"电影感", "胶片质感", "HDR", "3D 国漫 CG", "冷色调", "高对比度", "硬光剪影", "镜头光晕"},
			"rule":      "脚本节点和视频节点只能从 reference.md 词库选词，不自造镜头语言或风格词。",
			"must_list": "脚本输出 reference_terms_used，方便视频节点复用并做搭配验证。",
		},
		"creative_audit_contract": map[string]any{
			"memory_point":   "观众必须记住：古代权臣用朝堂识人术看穿现代谈判底牌",
			"unexpectedness": "必须有现代人缺少而古人独有的能力反杀，不只是换装穿越",
			"emotion":        "情绪从濒死、受辱、冷醒、反杀到旧爱重逢",
			"narrative":      "每7.5秒都必须完成从A到B的变化，不能只是静态展示",
			"pass_required":  true,
		},
		"compatibility_check_contract": map[string]any{
			"check_items": []string{"角色参考图是否与角色身份一致", "场景首帧是否能承接剧情", "运镜是否适合竖屏短剧", "prompt 是否前7.5秒完成动作", "是否避免真实政治人物和写实真人脸"},
			"repair_rule": "如不协调，脚本节点必须在 compatibility_check 中给出修正后的 seedance_prompt，而不是继续输出平庸提示词。",
		},
		"seedance_contract": map[string]any{
			"image_references": []string{"@图片1为场景背景参考", "@图片2为男主谢无咎参考", "@图片3为女帝萧明凰参考", "@图片4为现代外交官林晚棠参考", "@图片5为米国代表亚伦·霍克参考"},
			"prompt_language":  "中文",
			"video_prefix":     "9:16，30fps，15秒，Seedance 2.0，电影感，3D 国漫 CG，HDR",
			"must_include":     []string{"读取 creative_audit", "读取 scenes[i].seedance_prompt", "读取 reference_terms_used", "读取 compatibility_check", "前7.5秒完成有效剧情", "打脸台词", "环境音或音桥", "禁止文字/字幕/LOGO/水印"},
		},
	}
	for key, value := range fields {
		if err := setTemplateNodeJSONField(node, key, value); err != nil {
			return fmt.Errorf("set global chairman %s: %w", key, err)
		}
	}
	return nil
}

func setTemplateNodePrompt(node *Node, prompt string) error {
	return setTemplateNodeStringField(node, "prompt", prompt)
}

func setTemplateNodeStringField(node *Node, key, value string) error {
	return setTemplateNodeJSONField(node, key, value)
}

func setTemplateNodeJSONField(node *Node, key string, value any) error {
	var config map[string]json.RawMessage
	if len(node.Config) > 0 {
		if err := json.Unmarshal(node.Config, &config); err != nil {
			return err
		}
	}
	if config == nil {
		config = make(map[string]json.RawMessage)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	config[key] = encoded
	node.Config, err = json.Marshal(config)
	return err
}

func findTemplateNodeIndex(graph Graph, id string) int {
	for i := range graph.Nodes {
		if graph.Nodes[i].ID == id {
			return i
		}
	}
	return -1
}

func ensureTemplateNodeInput(graph *Graph, nodeID string, port Port) error {
	index := findTemplateNodeIndex(*graph, nodeID)
	if index < 0 {
		return fmt.Errorf("template node %s not found", nodeID)
	}
	for _, existing := range graph.Nodes[index].Inputs {
		if existing.ID == port.ID {
			return nil
		}
	}
	graph.Nodes[index].Inputs = append(graph.Nodes[index].Inputs, port)
	return nil
}

func ensureTemplateEdge(graph *Graph, edge Edge) error {
	for _, existing := range graph.Edges {
		if existing.ID == edge.ID {
			return nil
		}
	}
	if findTemplateNodeIndex(*graph, edge.Source) < 0 {
		return fmt.Errorf("template edge %s source %s not found", edge.ID, edge.Source)
	}
	if findTemplateNodeIndex(*graph, edge.Target) < 0 {
		return fmt.Errorf("template edge %s target %s not found", edge.ID, edge.Target)
	}
	graph.Edges = append(graph.Edges, edge)
	return nil
}

func reorderTemplateInputs(graph *Graph, nodeID string, order []string) {
	index := findTemplateNodeIndex(*graph, nodeID)
	if index < 0 {
		return
	}
	inputs := graph.Nodes[index].Inputs
	if len(inputs) == 0 {
		return
	}
	byID := make(map[string]Port, len(inputs))
	for _, input := range inputs {
		byID[input.ID] = input
	}
	next := make([]Port, 0, len(inputs))
	used := make(map[string]bool, len(inputs))
	for _, id := range order {
		if input, ok := byID[id]; ok {
			next = append(next, input)
			used[id] = true
		}
	}
	for _, input := range inputs {
		if !used[input.ID] {
			next = append(next, input)
		}
	}
	graph.Nodes[index].Inputs = next
}

func UpgradeGraphV1ToV2(source Graph) (Graph, error) {
	graph, err := CloneGraph(source)
	if err != nil {
		return Graph{}, err
	}
	if graph.SchemaVersion == SchemaVersionV2 {
		return graph, nil
	}
	if graph.SchemaVersion != SchemaVersionV1 {
		return Graph{}, fmt.Errorf("unsupported graph schema version %d", graph.SchemaVersion)
	}
	graph.SchemaVersion = SchemaVersionV2
	graph.Settings.Resolution = Resolution720p
	graph.Settings.SceneDurationMS = graph.Settings.EffectiveSceneDurationMS()
	if graph.Settings.SceneDurationMS == 0 {
		graph.Settings.SceneDurationMS = SceneDurationMS
	}
	graph.Settings.SceneDurationSeconds = 0
	graph.Settings.CharacterApprovalPolicy = ApprovalManual
	graph.Settings.StoryboardApprovalPolicy = ApprovalManual
	for i := range graph.Nodes {
		graph.Nodes[i].PositionMode = PositionAuto
		if graph.Nodes[i].Type != NodeTimeline {
			continue
		}
		var config TimelineConfig
		if err := json.Unmarshal(graph.Nodes[i].Config, &config); err != nil {
			return Graph{}, fmt.Errorf("decode timeline config: %w", err)
		}
		config.Clips = normalizedTimelineClips(config)
		config.ClipNodeIDs = nil
		graph.Nodes[i].Config, err = json.Marshal(config)
		if err != nil {
			return Graph{}, err
		}
	}
	return graph, nil
}

func blankVideoCanvasTemplate() Template {
	settings := Settings{
		AspectRatio: AspectRatioPortrait, Resolution: Resolution720p, FPS: DefaultFPS,
		SceneDurationMS: SceneDurationMS, CharacterApprovalPolicy: ApprovalManual,
		StoryboardApprovalPolicy: ApprovalManual, TextModel: "default", ImageModel: "gpt-image-2", VideoModel: "doubao-seedance-2-0-fast-260128",
	}
	timelineConfig, _ := json.Marshal(TimelineConfig{Clips: []TimelineClip{{
		ID: "clip_1", SourceNodeID: "video_1", SourcePort: "video", TrimOutMS: SceneDurationMS,
	}}})
	graph := Graph{SchemaVersion: SchemaVersionV2, Settings: settings}
	graph.Nodes = []Node{
		{ID: "brief", Type: NodeStoryBrief, Position: Position{X: 40, Y: 160}, Config: json.RawMessage(`{"title":"创意简报","prompt":"描述主题、人物、场景和镜头"}`), Outputs: []Port{{ID: "text", Type: PortText}}},
		{ID: "scene_1", Type: NodeScene, SceneID: "scene_1", DurationSeconds: SceneDuration, Position: Position{X: 300, Y: 160}, Inputs: []Port{{ID: "script", Type: PortText, Required: true}}, Outputs: []Port{{ID: "scene", Type: PortScene}}},
		{ID: "background_1", Type: NodeBackground, SceneID: "scene_1", Position: Position{X: 540, Y: 160}, Inputs: []Port{{ID: "environment", Label: "环境（地点/灯光/静物）", Type: PortScene, Required: true}}, Outputs: []Port{{ID: "image", Type: PortImage}}},
		{ID: "video_1", Type: NodeVideo, SceneID: "scene_1", DurationSeconds: SceneDuration, Position: Position{X: 800, Y: 160}, Inputs: []Port{{ID: "scene", Type: PortScene, Required: true}, {ID: "background", Type: PortImage, Required: true}}, Outputs: []Port{{ID: "video", Type: PortVideo}}},
		{ID: "timeline", Type: NodeTimeline, Locked: true, Position: Position{X: 1060, Y: 160}, Config: timelineConfig, Inputs: []Port{{ID: "clip_1", Type: PortVideo, Required: true}}, Outputs: []Port{{ID: "videos", Type: PortVideoList}}},
		{ID: "compose", Type: NodeCompose, Locked: true, Position: Position{X: 1300, Y: 160}, Inputs: []Port{{ID: "videos", Type: PortVideoList, Required: true}}, Outputs: []Port{{ID: "video", Type: PortVideo}}},
	}
	for i := range graph.Nodes {
		graph.Nodes[i].PositionMode = PositionAuto
	}
	graph.Edges = []Edge{
		{ID: "e_brief_scene", Source: "brief", SourcePort: "text", Target: "scene_1", TargetPort: "script"},
		{ID: "e_scene_background", Source: "scene_1", SourcePort: "scene", Target: "background_1", TargetPort: "environment"},
		{ID: "e_scene_video", Source: "scene_1", SourcePort: "scene", Target: "video_1", TargetPort: "scene"},
		{ID: "e_background_video", Source: "background_1", SourcePort: "image", Target: "video_1", TargetPort: "background"},
		{ID: "e_video_timeline", Source: "video_1", SourcePort: "video", Target: "timeline", TargetPort: "clip_1"},
		{ID: "e_timeline_compose", Source: "timeline", SourcePort: "videos", Target: "compose", TargetPort: "videos"},
	}
	graph.Groups = []Group{{ID: "scene_group_1", Type: "scene", SceneID: "scene_1", Enabled: true, DurationSeconds: SceneDuration, NodeIDs: []string{"scene_1", "background_1", "video_1"}, Position: Position{X: 280, Y: 110}, Size: Size{Width: 740, Height: 250}}}
	return Template{Code: "blank_video_canvas", Version: 1, Name: "空白视频画布", Description: "单场景图片、视频与成片工作流", Graph: graph, Enabled: true}
}

func CloneGraph(g Graph) (Graph, error) {
	raw, err := json.Marshal(g)
	if err != nil {
		return Graph{}, err
	}
	var cloned Graph
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return Graph{}, err
	}
	return cloned, nil
}
