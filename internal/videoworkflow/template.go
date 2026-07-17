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
	globalChairman, err := globalChairmanDramaTemplate(ancient)
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

func globalChairmanDramaTemplate(base Template) (Template, error) {
	graph, err := CloneGraph(base.Graph)
	if err != nil {
		return Template{}, err
	}
	graph.Settings.AspectRatio = AspectRatioPortrait
	graph.Settings.Resolution = Resolution1080p
	graph.Settings.FPS = DefaultFPS
	graph.Settings.SceneDurationMS = SceneDurationMS
	graph.Settings.SceneDurationSeconds = 0
	graph.Settings.CharacterApprovalPolicy = ApprovalAutoFirst
	graph.Settings.StoryboardApprovalPolicy = ApprovalAuto
	graph.Settings.TextModel = "default"
	graph.Settings.ImageModel = "gpt-image-2"
	graph.Settings.VideoModel = "doubao-seedance-2-0-fast-260128"
	if err := addGlobalChairmanRivalRole(&graph); err != nil {
		return Template{}, err
	}
	if err := applyGlobalChairmanDramaDefaults(&graph); err != nil {
		return Template{}, err
	}
	NormalizeBackgroundEnvironmentPorts(&graph)
	template := base
	template.Code = "global_chairman_drama_seedance"
	template.Version = 1
	template.Name = "古人穿越主席短剧 · 30秒单集"
	template.Description = "近现实架空短剧模板：古代权臣穿越纽港，依靠识人术、纵横术和乱世治理经验，在寰球联合会危机现场打脸升级；自动生成角色图、背景图、4段 Seedance 视频并裁剪合成约30秒单集。"
	template.SourceURL = ""
	template.Enabled = true
	template.Graph = graph
	return template, nil
}

func addGlobalChairmanRivalRole(graph *Graph) error {
	if findTemplateNodeIndex(*graph, "role_rival") < 0 {
		config := json.RawMessage(`{"name":"亚伦·霍克","adult_age":42,"candidate_count":2,"view":"全身三视图 + 脸部特写 + 服装物料细节"}`)
		graph.Nodes = append(graph.Nodes, Node{
			ID: "role_rival", Type: NodeCharacter, RoleID: "rival", Position: Position{X: 300, Y: 470}, PositionMode: PositionAuto, Config: config,
			Inputs:  []Port{{ID: "brief", Type: PortText, Required: true}},
			Outputs: []Port{{ID: "candidates", Type: PortImageSet}, {ID: "selected", Type: PortImage}},
		})
	}
	if err := ensureTemplateNodeInput(graph, "script", Port{ID: "rival", Type: PortImage, Required: true}); err != nil {
		return err
	}
	if err := ensureTemplateEdge(graph, Edge{ID: "e_brief_rival", Source: "brief", SourcePort: "text", Target: "role_rival", TargetPort: "brief"}); err != nil {
		return err
	}
	if err := ensureTemplateEdge(graph, Edge{ID: "e_rival_script", Source: "role_rival", SourcePort: "selected", Target: "script", TargetPort: "rival"}); err != nil {
		return err
	}
	for i := 1; i <= 4; i++ {
		videoID := fmt.Sprintf("video_%d", i)
		if err := ensureTemplateNodeInput(graph, videoID, Port{ID: "rival", Type: PortImage, Required: true}); err != nil {
			return err
		}
		if err := ensureTemplateEdge(graph, Edge{
			ID: fmt.Sprintf("e_rival_video_%d", i), Source: "role_rival", SourcePort: "selected",
			Target: videoID, TargetPort: "rival",
		}); err != nil {
			return err
		}
	}
	reorderTemplateInputs(graph, "script", []string{"brief", "hero", "heroine", "cousin", "rival"})
	for i := 1; i <= 4; i++ {
		reorderTemplateInputs(graph, fmt.Sprintf("video_%d", i), []string{"scene", "background", "hero", "heroine", "cousin", "rival"})
	}
	return nil
}

func applyGlobalChairmanDramaDefaults(graph *Graph) error {
	const characterSheetSuffix = "输出一张角色设定参考图：上排全身正面、侧面、背面三视图；中排脸部大特写（正面与3/4侧）刻画五官、发型、眼神、妆容或表情细节；下排服装与物料细节图（面料纹理、领口、徽章、配饰、鞋履、腰带或文件夹等材质特写）；浅底设定稿、布局清晰；统一面容、发型、服装和气质；虚构成年人；非照片、非写实真人脸、非真实政治人物。"
	characterPrompts := map[string]string{
		"role_hero":    "电影感3D国漫角色设定；虚构成年人男主谢无咎（30岁），大雍权臣穿越到现代，剑眉深目，长发束冠但外披旧黑色朝服，胸口有暗色血痕，眼神冷静压迫，气质像从朝堂和战场里活下来的人；核心竞争力是识人术、纵横术、朝堂辩论和乱世治理经验。" + characterSheetSuffix,
		"role_heroine": "电影感3D国漫角色设定；虚构成年人女帝萧明凰（28岁），古代帝王气场，玄金凤纹长袍与现代黑色大衣混搭，发髻插金簪，眼神高傲又带悔意；她曾赐死男主，现代重逢后成为情绪爆点。" + characterSheetSuffix,
		"role_cousin":  "电影感3D国漫角色设定；虚构成年人现代外交官林晚棠（27岁），华国青年外交官，利落短发，白色衬衫、深蓝西装、胸前工作证，手持平板和文件夹，眼神清醒坚定；她发现男主的古代纵横术有现代外交价值。" + characterSheetSuffix,
		"role_rival":   "电影感3D国漫角色设定；虚构成年人米国代表亚伦·霍克（42岁），银灰西装、鹰形领针、冷笑表情，坐姿强势，眼神精明多疑；他代表强势阵营，先嘲笑男主是古装疯子，后在会议现场被男主看穿底牌。" + characterSheetSuffix,
	}
	characterNames := map[string]string{
		"role_hero": "谢无咎", "role_heroine": "萧明凰", "role_cousin": "林晚棠", "role_rival": "亚伦·霍克",
	}
	backgroundPrompts := map[string]string{
		"background_1": "近未来电影感3D国漫场景背景；纽港雨夜，寰球联合会总部外广场，玻璃幕墙、万国旗、直播屏幕、警戒栏、路面积水反光；无人空镜或仅远景不可辨识剪影；非照片、非写实真人；9:16单镜头、非拼贴，保持真实都市空间层次。",
		"background_2": "近未来电影感3D国漫场景背景；纽港救助站与地下公共图书馆之间的夜间走廊，旧长椅、自动售货机、荧光灯、堆叠法律书和语言教材；无人空镜或仅远景不可辨识剪影；非照片、非写实真人；9:16单镜头、非拼贴。",
		"background_3": "近未来电影感3D国漫场景背景；寰球联合会危机会议厅，环形会议桌、同声传译耳机、蓝色电子席牌、巨幅世界地图屏幕、冷白顶光；无人空镜或仅远景不可辨识剪影；非照片、非写实真人；9:16单镜头、非拼贴。",
		"background_4": "近未来电影感3D国漫场景背景；寰球联合会主席台，万国旗成弧形排列，中央演讲台、聚光灯、玻璃穹顶、远处记者席灯光闪烁；无人空镜或仅远景不可辨识剪影；非照片、非写实真人；9:16单镜头、非拼贴。",
	}
	videoPrompts := map[string]string{
		"video_1": "9:16，30fps，15秒，电影感3D国漫短剧。参考图用途：@图片1为场景背景参考，@图片2为男主谢无咎参考，@图片3为女帝萧明凰参考，@图片4为现代外交官林晚棠参考，@图片5为米国代表亚伦·霍克参考。前8秒作为正片素材：闪白转场，古代毒酒入喉的记忆叠化到纽港雨夜，谢无咎穿染血朝服跌落在寰球联合会门口，围观直播手机灯亮起，低角度推镜头到他睁眼；情绪从濒死到冷醒。使用闪回、叠化、低角度、推镜头、胶片颗粒、冷色调。台词（围观者嘲讽）：“古装疯子还想进寰联？”禁止文字、字幕、LOGO、水印；虚构成年人；非照片、非写实真人脸。",
		"video_2": "9:16，30fps，15秒，电影感3D国漫短剧。参考图用途：@图片1为场景背景参考，@图片2为男主谢无咎参考，@图片3为女帝萧明凰参考，@图片4为现代外交官林晚棠参考，@图片5为米国代表亚伦·霍克参考。前8秒作为正片素材：救助站夜灯下，谢无咎被工作人员推开，下一秒他在地下图书馆疯狂翻阅国际法和各国史料，手指划过地图，眼神越来越锋利；林晚棠在远处注意到他把冲突国利益线画成古代合纵图。使用交叉蒙太奇、快切、特写、焦点转移、冷暖对比。台词（谢无咎低声）：“换了衣冠，权力还是那些权力。”禁止文字、字幕、LOGO、水印；虚构成年人；非照片、非写实真人脸。",
		"video_3": "9:16，30fps，15秒，电影感3D国漫短剧。参考图用途：@图片1为场景背景参考，@图片2为男主谢无咎参考，@图片3为女帝萧明凰参考，@图片4为现代外交官林晚棠参考，@图片5为米国代表亚伦·霍克参考。前8秒作为正片素材：危机会议厅内，亚伦拍桌冷笑，代表们争吵，谢无咎从翻译席缓缓起身，全场嘲笑瞬间被希区柯克变焦压成静默；他用眼神扫过每个人的手指、座次、停顿，像审早朝一样点破各方底牌。使用希区柯克变焦、环绕拍摄、特写、硬切、高对比度。台词（谢无咎冷声）：“诸位不是想停战，是在等一个体面认输的台阶。”禁止文字、字幕、LOGO、水印；虚构成年人；非照片、非写实真人脸。",
		"video_4": "9:16，30fps，15秒，电影感3D国漫短剧。参考图用途：@图片1为场景背景参考，@图片2为男主谢无咎参考，@图片3为女帝萧明凰参考，@图片4为现代外交官林晚棠参考，@图片5为米国代表亚伦·霍克参考。前8秒作为正片素材：主席台聚光灯亮起，亚伦表情僵住，林晚棠震惊抬眼，萧明凰从人群阴影里出现红了眼；谢无咎整理袖口走向演讲台，万国旗在身后展开。使用仰拍、拉镜头、慢动作、镜头光晕、硬光剪影。台词（谢无咎平静）：“陛下，当年你怕我掌一国；如今，我要调停天下。”结尾留悬念，禁止文字、字幕、LOGO、水印；虚构成年人；非照片、非写实真人脸。",
	}
	for i := range graph.Nodes {
		node := &graph.Nodes[i]
		var prompt string
		switch {
		case node.ID == "brief":
			prompt = "创建分集短剧《千年权臣：我在纽港执掌寰球联合会》第1集《古装疯子闯寰联》的30秒单集素材。近现实架空命名：米国、罗斯国、东瀛国、华国、纽港、鹰宫、五环楼、寰球联合会。男主谢无咎是大雍权臣，被女帝萧明凰赐死后穿越到现代；他的核心竞争力不是现代学历，而是古代乱世炼出的识人术、纵横术、朝堂辩论、帝王心术和赈灾治国经验。第1集四段：穿越落地被嘲笑、救助站受辱并夜学、危机会议看穿各国底牌、主席台金句反杀并埋女帝重逢悬念。风格为狗血短剧、强打脸、电影感3D国漫；所有角色均为虚构成年人；非照片、非写实真人脸、非真实政治人物。"
			if err := applyGlobalChairmanBriefAssetPackage(node); err != nil {
				return err
			}
		case node.ID == "script" || node.Type == NodeScript:
			prompt = "基于上游材料包，仅输出严格 JSON，不要 Markdown。格式为 {\"title\":\"千年权臣：我在纽港执掌寰球联合会 第1集\",\"scenes\":[...]}。生成4个 scenes，每个 scene 固定15秒，但前7.5秒必须完成有效剧情动作，供时间线裁剪合成约30秒。每个 scene 包含 index、location、time、summary、characters、dialogue、camera、action、expression、lighting、audio、image_prompt、video_prompt。剧情：1穿越落地被米国网红嘲笑；2救助站受辱后夜学现代规则，林晚棠发现他的合纵图；3寰球联合会危机会议，亚伦·霍克嘲讽，谢无咎用识人术点破各方底牌；4主席台前金句打脸，萧明凰在阴影里重逢。必须使用架空名：米国、罗斯国、东瀛国、华国、纽港、寰球联合会。角色均为虚构成年人；非照片、非写实真人脸。"
		case characterPrompts[node.ID] != "":
			prompt = characterPrompts[node.ID]
			if err := setTemplateNodeStringField(node, "name", characterNames[node.ID]); err != nil {
				return fmt.Errorf("set global chairman character name for %s: %w", node.ID, err)
			}
			if err := setTemplateNodeStringField(node, "view", "全身三视图 + 脸部特写 + 服装物料细节"); err != nil {
				return fmt.Errorf("set global chairman view for %s: %w", node.ID, err)
			}
		case backgroundPrompts[node.ID] != "":
			prompt = backgroundPrompts[node.ID]
		case node.Type == NodeBackground:
			prompt = "生成近现实架空短剧场景背景；无人空镜或仅远景不可辨识剪影；禁止近中景人物与肢体动作；道具仅可静置；非照片、非写实真人脸；9:16单镜头、非拼贴，保持电影感3D国漫美术与清晰空间层次。"
		case videoPrompts[node.ID] != "":
			prompt = videoPrompts[node.ID]
		case node.Type == NodeVideo:
			prompt = "9:16，30fps，15秒，电影感3D国漫短剧。参考图用途：@图片1为场景背景参考，@图片2为男主谢无咎参考，@图片3为女帝萧明凰参考，@图片4为现代外交官林晚棠参考，@图片5为米国代表亚伦·霍克参考。前8秒作为正片素材，动作和情绪必须前置完成；禁止文字、字幕、LOGO、水印；虚构成年人；非照片、非写实真人脸。"
		case node.Type == NodeTimeline:
			var config TimelineConfig
			if err := json.Unmarshal(node.Config, &config); err != nil {
				return fmt.Errorf("decode global chairman timeline: %w", err)
			}
			if len(config.Clips) != 4 {
				return fmt.Errorf("global chairman timeline has %d clips, want 4", len(config.Clips))
			}
			for i := range config.Clips {
				config.Clips[i].TrimInMS = 0
				config.Clips[i].TrimOutMS = 7_500
			}
			encodedConfig, err := json.Marshal(config)
			if err != nil {
				return err
			}
			node.Config = encodedConfig
		}
		if prompt != "" {
			if err := setTemplateNodePrompt(node, prompt); err != nil {
				return fmt.Errorf("set global chairman prompt for %s: %w", node.ID, err)
			}
		}
	}
	return nil
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
		"series_bible": map[string]any{
			"title":        "千年权臣：我在纽港执掌寰球联合会",
			"format":       "近现实架空竖屏短剧；单集约30秒；4段7.5秒高密度素材",
			"theme":        "古代乱世技能在现代国际秩序中的降维打击",
			"style":        "电影感3D国漫；冷色现代会议空间与古代血色闪回对比；狗血短剧节奏",
			"memory_point": "男主用古代朝堂识人术看穿现代谈判底牌",
			"emotion_arc":  []string{"濒死穿越", "受辱夜学", "会议反杀", "主席台金句"},
			"world_terms":  []string{"米国", "罗斯国", "东瀛国", "华国", "纽港", "鹰宫", "五环楼", "寰球联合会"},
			"safety":       []string{"虚构成年人", "近现实架空", "非照片", "非写实真人脸", "非真实政治人物", "禁止字幕/LOGO/水印"},
		},
		"character_bible": []map[string]any{
			{
				"id": "hero", "node_id": "role_hero", "name": "谢无咎", "age": 30, "role": "古代权臣男主",
				"motivation": "在现代活下去，并证明权力可以用来止战而不是夺命", "core_skill": []string{"识人术", "纵横术", "朝堂辩论", "乱世治理", "帝王心术"},
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
		"scene_bible": []map[string]any{
			{"id": "arrival_plaza", "node_id": "background_1", "name": "纽港寰球联合会总部外广场", "time": "雨夜", "lighting": "冷蓝玻璃幕墙、手机直播灯、路面积水反光", "props": []string{"万国旗", "警戒栏", "直播屏幕", "手机灯"}, "rule": "无人空镜或远景剪影"},
			{"id": "night_study", "node_id": "background_2", "name": "救助站与地下公共图书馆走廊", "time": "深夜", "lighting": "荧光灯与自动售货机冷光", "props": []string{"旧长椅", "国际法书", "地图", "平板"}, "rule": "无人空镜"},
			{"id": "crisis_hall", "node_id": "background_3", "name": "寰球联合会危机会议厅", "time": "白天会议", "lighting": "冷白顶光与蓝色电子席牌", "props": []string{"环形会议桌", "同声传译耳机", "世界地图屏幕"}, "rule": "空场景，不出现可辨识人物"},
			{"id": "chair_podium", "node_id": "background_4", "name": "寰球联合会主席台", "time": "聚光灯时刻", "lighting": "主席台聚光灯、旗帜背光、记者席闪光", "props": []string{"中央演讲台", "万国旗", "玻璃穹顶"}, "rule": "空镜无特写人物"},
		},
		"episode_route": []map[string]any{
			{"episode_id": "ep_1", "scene_id": "scene_1", "title": "穿越落地", "goal": "用毒酒闪回和纽港雨夜建立强钩子", "conflict": "古代权臣被现代人当成古装疯子", "hook": "他睁眼时听见寰联警报"},
			{"episode_id": "ep_1", "scene_id": "scene_2", "title": "受辱夜学", "goal": "证明核心竞争力不是学历，而是把现代规则翻译成古代权力图谱", "conflict": "救助站羞辱与身份黑户", "hook": "林晚棠发现他的合纵图"},
			{"episode_id": "ep_1", "scene_id": "scene_3", "title": "会议反杀", "goal": "用识人术和纵横术打脸米国代表", "conflict": "亚伦嘲讽他不懂现代文明", "hook": "谢无咎点破各方不是想停战而是想体面下台"},
			{"episode_id": "ep_1", "scene_id": "scene_4", "title": "主席台悬念", "goal": "用金句完成爽点并埋女帝重逢", "conflict": "旧日赐死者亲眼看见他被万国注视", "hook": "萧明凰在人群阴影中出现"},
		},
		"storyboard_contract": map[string]any{
			"scene_count":            4,
			"duration_seconds_each":  15,
			"effective_trim_ms_each": 7500,
			"final_duration_ms":      30000,
			"time_ranges":            []string{"0-2秒", "2-5秒", "5-7.5秒", "7.5-15秒余韵"},
			"shot_fields":            []string{"index", "location", "time", "summary", "characters", "dialogue", "camera", "action", "expression", "lighting", "audio", "image_prompt", "video_prompt"},
			"allowed_camera_terms":   []string{"大远景", "全景", "中景", "近景", "特写", "推镜头", "拉镜头", "环绕拍摄", "低角度", "仰拍", "希区柯克变焦", "快切", "闪回", "叠化", "硬切", "镜头光晕"},
		},
		"seedance_contract": map[string]any{
			"image_references": []string{"@图片1为场景背景参考", "@图片2为男主谢无咎参考", "@图片3为女帝萧明凰参考", "@图片4为现代外交官林晚棠参考", "@图片5为米国代表亚伦·霍克参考"},
			"prompt_language":  "中文",
			"video_prefix":     "9:16，30fps，15秒，电影感3D国漫短剧",
			"must_include":     []string{"前8秒完成有效剧情", "打脸台词", "环境音或音桥", "禁止字幕/LOGO/水印"},
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
