<script setup>
import { computed, ref } from 'vue';
import { verifyCode } from '../api';

const code = ref('');
const result = ref(null);
const error = ref('');
const busy = ref(false);

const issuedAtText = computed(() =>
  result.value?.issued_at ? new Date(result.value.issued_at).toLocaleString() : '',
);

async function submit() {
  error.value = '';
  result.value = null;
  busy.value = true;
  try {
    // 核验结论完全来自服务端真实判据：失败时服务端返回 422 与原因，
    // 前端只展示，不自行放行、不还原坐标。
    result.value = await verifyCode(code.value.trim());
  } catch (e) {
    error.value = e.message;
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <section>
    <h2>核验短码</h2>
    <p class="hint">
      接收席输入无线电抄收到的九字符短码（八位数字 + 一位 0–9 或 X 校验符）。校验失败一律拒绝，不还原坐标。
    </p>
    <form @submit.prevent="submit">
      <label for="input-code">短码</label>
      <input
        id="input-code"
        v-model="code"
        data-testid="input-code"
        maxlength="9"
        placeholder="例如 152637483"
        autocomplete="off"
        style="text-transform: none"
      />
      <button type="submit" :disabled="busy">{{ busy ? '核验中…' : '核验' }}</button>
    </form>

    <p v-if="error" class="error" data-testid="verify-error" role="alert">拒绝：{{ error }}</p>

    <div v-if="result" class="card" data-testid="verify-result">
      <h3>核验通过</h3>
      <dl>
        <dt>唯一坐标</dt>
        <dd>
          X <span data-testid="verify-x">{{ result.x }}</span> 米， Y
          <span data-testid="verify-y">{{ result.y }}</span> 米
        </dd>
        <dt>短码</dt>
        <dd class="code" data-testid="verify-code">{{ result.code }}</dd>
        <dt>签发记录</dt>
        <dd data-testid="verify-issued">
          {{ result.issued ? `本台已签发（${issuedAtText}）` : '本台未签发过' }}
        </dd>
      </dl>
    </div>
  </section>
</template>
