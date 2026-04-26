import fs from 'node:fs/promises';
import path from 'node:path';

const root = path.resolve(path.dirname(new URL(import.meta.url).pathname), '..');
const baseURL = process.env.ECOMMERCE_BASE_URL || 'http://43.134.21.160:8080';
const token = process.env.ECOMMERCE_BEARER_TOKEN;

if (!token) {
  console.error('missing ECOMMERCE_BEARER_TOKEN');
  process.exit(1);
}

const metadataPath = path.join(root, 'assets/gpfa_server_materials/metadata_deduped.json');
const outputPath = path.join(root, 'assets/gpfa_server_materials/created_remote_tasks.json');
const metadata = JSON.parse(await fs.readFile(metadataPath, 'utf8'));
const uniqueItems = metadata.uniqueItems || [];

const plan = [
  {
    itemIndex: 0,
    platformCode: 'taobao',
    promptCode: 'conversion',
    styleCode: 'clean',
    scenario: '淘宝主图转化',
    note: '输出中文标题与卖点，突出稳定、参数清晰、采购决策效率。'
  },
  {
    itemIndex: 4,
    platformCode: 'jd',
    promptCode: 'premium',
    styleCode: 'tech_minimal',
    scenario: '京东参数型详情页',
    note: '输出中文，强调品牌可信度、企业级可靠性、参数信息层级。'
  },
  {
    itemIndex: 6,
    platformCode: 'douyin',
    promptCode: 'live_sale',
    styleCode: 'bold',
    scenario: '抖音直播带货页',
    note: '输出中文，强化短视频挂车转化、利益点和节奏感。'
  },
  {
    itemIndex: 3,
    platformCode: 'xiaohongshu',
    promptCode: 'content_seed',
    styleCode: 'fresh_lifestyle',
    scenario: '小红书种草内容',
    note: '输出中文，偏内容种草与使用场景表达，但保持服务器产品的专业感。'
  },
  {
    itemIndex: 1,
    platformCode: 'amazon',
    promptCode: 'cross_border',
    styleCode: 'premium_dark',
    scenario: 'Amazon 跨境详情页',
    note: '输出英文标题、卖点与图像规划，符合跨境平台规范，避免夸大承诺。'
  }
];

function request(url, init = {}) {
  return fetch(url, {
    ...init,
    headers: {
      Accept: 'application/json, text/plain, */*',
      Authorization: `Bearer ${token}`,
      Referer: `${baseURL}/personal/ecommerce-v2`,
      'User-Agent': 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36',
      ...(init.headers || {}),
    },
  });
}

function asMap(items) {
  return new Map((items || []).map((item) => [item.code, item]));
}

function mimeFromExt(file) {
  const ext = path.extname(file).toLowerCase();
  if (ext === '.png') return 'image/png';
  if (ext === '.jpg' || ext === '.jpeg') return 'image/jpeg';
  if (ext === '.webp') return 'image/webp';
  return 'application/octet-stream';
}

async function toDataURL(relPath) {
  const fullPath = path.join(root, 'assets/gpfa_server_materials', relPath);
  const buf = await fs.readFile(fullPath);
  return `data:${mimeFromExt(fullPath)};base64,${buf.toString('base64')}`;
}

function buildRequirement(item, step) {
  return [
    `任务名称：${step.scenario}`,
    `商品名称：${item.name}`,
    `品牌：${item.brand || '未标注'}`,
    `价格参考：${item.price != null ? `${item.price}元` : '未标注'}`,
    '品类：通用服务器',
    '参考图：基于已上传实拍/白底素材，保持真实外观和机架服务器结构。',
    '目标：用于展示电商平台覆盖能力，同一工作台输出不同平台适配结果。',
    step.note,
  ].join('\n');
}

const optionsRes = await request(`${baseURL}/api/me/ecommerce/options`);
const optionsJson = await optionsRes.json();
if (!optionsRes.ok || optionsJson.code !== 0) {
  console.error(JSON.stringify(optionsJson, null, 2));
  process.exit(1);
}

const platformMap = asMap(optionsJson.data.platforms);
const promptMap = asMap(optionsJson.data.prompt_templates);
const styleMap = asMap(optionsJson.data.style_templates);
const created = [];

for (const step of plan) {
  const item = uniqueItems[step.itemIndex];
  if (!item) throw new Error(`item index out of range: ${step.itemIndex}`);
  const platform = platformMap.get(step.platformCode);
  const prompt = promptMap.get(step.promptCode);
  const style = styleMap.get(step.styleCode);
  if (!platform || !prompt || !style) {
    throw new Error(`option missing: ${step.platformCode}/${step.promptCode}/${step.styleCode}`);
  }

  const body = {
    platform_id: platform.id,
    prompt_template_id: prompt.id,
    style_template_id: style.id,
    requirement: buildRequirement(item, step),
    reference_images: [await toDataURL(item.dedupedImage)],
  };

  const res = await request(`${baseURL}/api/me/ecommerce/tasks`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(body),
  });
  const json = await res.json();
  if (!res.ok || json.code !== 0) {
    console.error(JSON.stringify({ step, response: json }, null, 2));
    process.exit(1);
  }

  created.push({
    scenario: step.scenario,
    platform: platform.name,
    prompt: prompt.name,
    style: style.name,
    itemName: item.name,
    taskID: json.data.task_id,
    status: json.data.status,
    createdAt: json.data.created_at,
  });

  console.log(`${platform.name} | ${prompt.name} | ${style.name} | ${json.data.task_id}`);
  await new Promise((resolve) => setTimeout(resolve, 1200));
}

await fs.writeFile(outputPath, JSON.stringify({
  created_at: new Date().toISOString(),
  base_url: baseURL,
  tasks: created,
}, null, 2));

console.log(`saved ${created.length} tasks -> ${outputPath}`);
