<script setup>
import { onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { listCards } from '../api';

// 与后端默认每页数量一致；后端会再次强制上限。
const PAGE_SIZE = 20;

const router = useRouter();

const cards = ref([]);
const hasMore = ref(false);
const snapshotId = ref(null);
// nextCursor 只保存服务端签发的不透明游标，前端不解析、不构造。
const nextCursor = ref('');
const loading = ref(false);
const pageError = ref('');
const fatal = ref(''); // 首批就失败：尚无内容可保留
const started = ref(false);

function formatAt(iso) {
  return new Date(iso).toLocaleString();
}

// load 拉取一页：append=true 表示翻页。翻页失败只就地提示，绝不清空已
// 加载内容；重试时原样回传同一个游标（服务端保证同游标返回同一页）。
async function load(append) {
  if (loading.value) return;
  loading.value = true;
  pageError.value = '';
  const cursor = append ? nextCursor.value : '';
  try {
    const page = await listCards({ limit: PAGE_SIZE, cursor });
    if (append) {
      cards.value.push(...page.cards);
    } else {
      cards.value = page.cards;
    }
    hasMore.value = Boolean(page.has_more);
    nextCursor.value = page.next_cursor || '';
    snapshotId.value = page.snapshot_id ?? snapshotId.value;
  } catch (e) {
    if (append) {
      // 就地提示并保留全部已加载行；按钮仍可重试同一页。
      pageError.value = e.message;
    } else {
      fatal.value = e.message;
    }
  } finally {
    loading.value = false;
    started.value = true;
  }
}

function retryPage() {
  load(true);
}

function restartSnapshot() {
  fatal.value = '';
  pageError.value = '';
  load(false);
}

// 把选中的短码带入核验页：只预填输入框，绝不自动提交。
function bringToVerify(code) {
  router.push({ name: 'verify', query: { code } });
}

onMounted(() => load(false));
</script>

<template>
  <section>
    <h2>签发记录</h2>
    <p class="hint">
      按签发时间倒序连续查看当班记录。打开时确定一次快照：浏览期间新签发的卡不会插入当前序列；继续加载可逐页回看更早记录。
    </p>
    <p v-if="snapshotId" class="snapshot" data-testid="snapshot-boundary">
      当前快照编号：{{ snapshotId }}
    </p>

    <p v-if="fatal" class="error" data-testid="records-fatal" role="alert">
      签发记录加载失败：{{ fatal }}
      <button type="button" class="inline-btn" data-testid="records-reload" @click="restartSnapshot">
        重新查询
      </button>
    </p>

    <p v-else-if="started && cards.length === 0" class="hint" data-testid="records-empty">
      还没有已签发的坐标卡。
    </p>

    <ul v-else class="records" data-testid="records-list">
      <li v-for="card in cards" :key="card.id" class="record" :data-id="card.id" data-testid="record-row">
        <div class="record-main">
          <span class="code" data-testid="record-code">{{ card.code }}</span>
          <span class="coord" data-testid="record-coord">
            X {{ card.x }} 米，Y {{ card.y }} 米
          </span>
          <span class="at" data-testid="record-at">{{ formatAt(card.issued_at) }}</span>
        </div>
        <button
          type="button"
          class="bring-btn"
          data-testid="bring-verify"
          @click="bringToVerify(card.code)"
        >
          带入核验
        </button>
      </li>
    </ul>

    <!-- 翻页失败就地提示：已加载行原样保留，可重试同一页；快照/游标失效时
         可选择重新查询确定新快照。 -->
    <p v-if="pageError" class="error" data-testid="records-page-error" role="alert">
      本页加载失败：{{ pageError }}
      <button type="button" class="inline-btn" data-testid="retry-page" @click="retryPage">
        重试本页
      </button>
      <button type="button" class="inline-btn" data-testid="restart-snapshot" @click="restartSnapshot">
        重新查询（新快照）
      </button>
    </p>

    <div v-if="hasMore && !fatal" class="more">
      <button type="button" data-testid="load-more" :disabled="loading" @click="load(true)">
        {{ loading ? '加载中…' : '加载更早记录' }}
      </button>
    </div>
  </section>
</template>

<style scoped>
.snapshot {
  color: #94a3b8;
  font-size: 0.8rem;
  margin: 0.25rem 0 0.75rem;
}
.records {
  list-style: none;
  margin: 0.5rem 0 0;
  padding: 0;
}
.record {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  padding: 0.65rem 0.2rem;
  border-bottom: 1px solid #e2e8f0;
}
.record-main {
  display: flex;
  align-items: baseline;
  gap: 0.9rem;
  flex-wrap: wrap;
}
.record .code {
  font-size: 1.15rem;
}
.coord {
  color: #334155;
}
.at {
  color: #64748b;
  font-size: 0.85rem;
}
.bring-btn {
  margin: 0;
  padding: 0.35rem 0.9rem;
  font-size: 0.9rem;
  background: #0f766e;
  white-space: nowrap;
}
.more {
  margin-top: 0.75rem;
}
.inline-btn {
  margin: 0 0 0 0.6rem;
  padding: 0.2rem 0.7rem;
  font-size: 0.85rem;
  background: #2563eb;
}
</style>
