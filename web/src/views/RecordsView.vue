<script setup>
import { onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { listCards } from '../api';

// 每页数量与服务端默认一致；服务端另有上限保护。
const PAGE_SIZE = 20;

const router = useRouter();
const records = ref([]);
const snapshot = ref('');
const nextCursor = ref(null);
const hasMore = ref(false);
const loading = ref(false);
const loaded = ref(false); // 首批是否成功过（区分初次加载失败与空记录）
const error = ref('');

function formatTime(iso) {
  return new Date(iso).toLocaleString();
}

// 请求一页；失败时保留已加载内容，游标不前移，可重试同一页。
async function fetchPage(params) {
  loading.value = true;
  error.value = '';
  try {
    return await listCards(params);
  } catch (e) {
    error.value = e.message;
    return null;
  } finally {
    loading.value = false;
  }
}

async function loadFirst() {
  const data = await fetchPage({ limit: PAGE_SIZE });
  if (!data) return;
  records.value = data.cards;
  snapshot.value = data.snapshot;
  nextCursor.value = data.next_cursor;
  hasMore.value = data.has_more;
  loaded.value = true;
}

async function loadMore() {
  if (!nextCursor.value) return;
  const data = await fetchPage({
    limit: PAGE_SIZE,
    snapshot: snapshot.value,
    cursor: nextCursor.value,
  });
  if (!data) return;
  records.value = records.value.concat(data.cards);
  snapshot.value = data.snapshot;
  nextCursor.value = data.next_cursor;
  hasMore.value = data.has_more;
}

// 重试同一页：首批失败重新取首页；翻页失败用原游标重试该页。
function retry() {
  if (nextCursor.value) {
    loadMore();
  } else {
    loadFirst();
  }
}

// 重新加载：放弃当前快照，以最新记录为边界重新开始。
function reload() {
  records.value = [];
  snapshot.value = '';
  nextCursor.value = null;
  hasMore.value = false;
  loadFirst();
}

// 把选中的短码带入核验页；只预填，不自动提交。
function carryToVerify(code) {
  router.push({ name: 'verify', query: { code } });
}

onMounted(loadFirst);
</script>

<template>
  <section>
    <h2>签发记录</h2>
    <p class="hint">
      按签发时间倒序展示本台已签发的坐标卡，交接班时可逐条核对近期口述内容。
      浏览期间新签发的卡不会插入当前序列；点击「带去核验」把短码填入核验页（不会自动提交）。
    </p>

    <div class="toolbar">
      <button type="button" data-testid="records-reload" :disabled="loading" @click="reload">
        重新加载
      </button>
    </div>

    <p v-if="loaded && !records.length && !error" class="hint" data-testid="records-empty">
      本班还没有签发记录。
    </p>

    <ul v-if="records.length" class="records" data-testid="records-list">
      <li v-for="card in records" :key="card.id" class="record-row" data-testid="record-row">
        <div class="record-main">
          <span class="code record-code" data-testid="record-code">{{ card.code }}</span>
          <span class="record-coord" data-testid="record-coord">
            X {{ card.x }} 米，Y {{ card.y }} 米
          </span>
          <span class="record-time" data-testid="record-time">{{ formatTime(card.issued_at) }}</span>
        </div>
        <button
          type="button"
          class="carry"
          data-testid="carry-verify"
          @click="carryToVerify(card.code)"
        >
          带去核验
        </button>
      </li>
    </ul>

    <p v-if="loading" class="hint" data-testid="records-loading">加载中…</p>

    <div v-if="error" class="error" data-testid="records-error" role="alert">
      <p class="error-text">{{ error }}</p>
      <button type="button" data-testid="records-retry" :disabled="loading" @click="retry">
        重试
      </button>
    </div>

    <button
      v-if="hasMore && !loading"
      type="button"
      data-testid="load-more"
      @click="loadMore"
    >
      加载更早记录
    </button>
    <p v-else-if="loaded && records.length && !error" class="hint" data-testid="records-end">
      已显示全部记录
    </p>
  </section>
</template>

<style scoped>
.toolbar {
  display: flex;
  justify-content: flex-end;
}
.toolbar button {
  margin-top: 0;
  padding: 0.35rem 1rem;
  font-size: 0.9rem;
  background: #e2e8f0;
  color: #1f2933;
}
.records {
  list-style: none;
  margin: 1rem 0 0;
  padding: 0;
}
.record-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  padding: 0.6rem 0;
  border-bottom: 1px solid #e2e8f0;
}
.record-main {
  display: flex;
  flex-direction: column;
  gap: 0.15rem;
  min-width: 0;
}
.record-code {
  font-size: 1.15rem;
}
.record-coord {
  color: #334155;
  font-size: 0.95rem;
}
.record-time {
  color: #94a3b8;
  font-size: 0.8rem;
}
.carry {
  margin-top: 0;
  flex-shrink: 0;
  padding: 0.4rem 0.9rem;
  font-size: 0.9rem;
  background: #0d9488;
}
.error {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
}
.error-text {
  margin: 0;
}
.error button {
  margin-top: 0;
  flex-shrink: 0;
  padding: 0.35rem 1rem;
  font-size: 0.9rem;
  background: #b91c1c;
}
</style>
