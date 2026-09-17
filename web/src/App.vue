<template>
  <div class="layout">
    <header>
      <h1>山地搜救坐标短码</h1>
      <nav>
        <RouterLink to="/issue" data-testid="nav-issue">签发席</RouterLink>
        <RouterLink to="/records" data-testid="nav-records">签发记录</RouterLink>
        <RouterLink to="/verify" data-testid="nav-verify">核验席</RouterLink>
      </nav>
    </header>
    <main>
      <RouterView />
    </main>
    <footer>
      <p>九字符短码：八位交错主体 + 一位模 11 校验符（余数十写作 X），任何单字符抄错都会被拒绝。</p>
    </footer>
  </div>
</template>

<style>
:root {
  color-scheme: light;
  font-family: 'PingFang SC', 'Microsoft YaHei', system-ui, sans-serif;
}
body {
  margin: 0;
  background: #f4f6f8;
  color: #1f2933;
}
.layout {
  max-width: 640px;
  margin: 0 auto;
  padding: 1.5rem 1rem 3rem;
}
header h1 {
  font-size: 1.4rem;
  margin: 0 0 0.75rem;
}
nav {
  display: flex;
  gap: 0.5rem;
  margin-bottom: 1.25rem;
}
nav a {
  padding: 0.4rem 1rem;
  border-radius: 6px;
  background: #e2e8f0;
  color: #1f2933;
  text-decoration: none;
}
nav a.router-link-active {
  background: #2563eb;
  color: #fff;
}
section {
  background: #fff;
  border-radius: 10px;
  padding: 1.25rem 1.5rem 1.5rem;
  box-shadow: 0 1px 3px rgb(0 0 0 / 0.08);
}
.hint {
  color: #64748b;
  font-size: 0.9rem;
}
form label {
  display: block;
  margin: 0.75rem 0 0.25rem;
  font-weight: 600;
}
form input {
  width: 100%;
  box-sizing: border-box;
  padding: 0.55rem 0.7rem;
  font-size: 1.05rem;
  border: 1px solid #cbd5e1;
  border-radius: 6px;
  letter-spacing: 0.08em;
}
button {
  margin-top: 1rem;
  padding: 0.55rem 1.6rem;
  font-size: 1rem;
  border: none;
  border-radius: 6px;
  background: #2563eb;
  color: #fff;
  cursor: pointer;
}
button:disabled {
  background: #94a3b8;
  cursor: wait;
}
.error {
  margin-top: 1rem;
  padding: 0.7rem 0.9rem;
  border-radius: 6px;
  background: #fef2f2;
  border: 1px solid #fca5a5;
  color: #b91c1c;
  font-weight: 600;
}
.card {
  margin-top: 1.25rem;
  padding: 1rem 1.1rem;
  border-radius: 8px;
  background: #f0fdf4;
  border: 1px solid #86efac;
}
.card h3 {
  margin: 0 0 0.5rem;
  color: #15803d;
}
.card dl {
  display: grid;
  grid-template-columns: 5.5em 1fr;
  gap: 0.35rem 0.75rem;
  margin: 0;
}
.card dt {
  color: #64748b;
}
.card dd {
  margin: 0;
}
.code {
  font-family: 'JetBrains Mono', Consolas, monospace;
  font-size: 1.5rem;
  font-weight: 700;
  letter-spacing: 0.2em;
}
.preview {
  color: #475569;
}
.preview strong {
  font-family: Consolas, monospace;
  letter-spacing: 0.15em;
}
footer {
  margin-top: 1.5rem;
  color: #94a3b8;
  font-size: 0.8rem;
}
</style>
