<script setup>
import { computed, ref } from 'vue';
import { encode, parseCoordinate } from '../shortcode';
import { issueCard } from '../api';

const x = ref('');
const y = ref('');
const card = ref(null);
const error = ref('');
const busy = ref(false);

const xParsed = computed(() => parseCoordinate(x.value));
const yParsed = computed(() => parseCoordinate(y.value));

// 本地预检与服务端使用同一套判据（src/shortcode.js），仅用于即时提示；
// 签发结果一律以服务端真实响应为准。
const inputError = computed(() => {
  if (x.value.trim() === '' || y.value.trim() === '') return '';
  if (xParsed.value === null || yParsed.value === null) {
    return '坐标须为 0 至 9999 的十进制整数米';
  }
  return '';
});

const preview = computed(() => {
  if (inputError.value || xParsed.value === null || yParsed.value === null) return '';
  return encode(xParsed.value, yParsed.value);
});

const issuedAtText = computed(() =>
  card.value ? new Date(card.value.issued_at).toLocaleString() : '',
);

async function submit() {
  error.value = '';
  card.value = null;
  if (xParsed.value === null || yParsed.value === null) {
    error.value = '坐标须为 0 至 9999 的十进制整数米';
    return;
  }
  busy.value = true;
  try {
    card.value = await issueCard(xParsed.value, yParsed.value);
  } catch (e) {
    error.value = e.message;
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <section>
    <h2>签发坐标卡</h2>
    <p class="hint">
      输入目标点坐标（单位：米，x、y 各为 0–9999 的十进制整数），签发后通过无线电口述九字符短码。
    </p>
    <form @submit.prevent="submit">
      <label for="input-x">X 坐标（米）</label>
      <input
        id="input-x"
        v-model="x"
        data-testid="input-x"
        inputmode="numeric"
        placeholder="0–9999"
        autocomplete="off"
      />
      <label for="input-y">Y 坐标（米）</label>
      <input
        id="input-y"
        v-model="y"
        data-testid="input-y"
        inputmode="numeric"
        placeholder="0–9999"
        autocomplete="off"
      />
      <p v-if="inputError" class="error" data-testid="input-error" role="alert">{{ inputError }}</p>
      <p v-else-if="preview" class="preview">
        短码预览：<strong data-testid="code-preview">{{ preview }}</strong>
      </p>
      <button type="submit" :disabled="busy">{{ busy ? '签发中…' : '签发' }}</button>
    </form>

    <p v-if="error" class="error" data-testid="issue-error" role="alert">{{ error }}</p>

    <div v-if="card" class="card" data-testid="issued-card">
      <h3>签发成功</h3>
      <dl>
        <dt>原坐标</dt>
        <dd>
          X <span data-testid="issued-x">{{ card.x }}</span> 米， Y
          <span data-testid="issued-y">{{ card.y }}</span> 米
        </dd>
        <dt>短码</dt>
        <dd class="code" data-testid="issued-code">{{ card.code }}</dd>
        <dt>签发时间</dt>
        <dd data-testid="issued-at">{{ issuedAtText }}</dd>
      </dl>
    </div>
  </section>
</template>
