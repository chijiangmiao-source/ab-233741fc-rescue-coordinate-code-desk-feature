import { createApp } from 'vue';
import { createRouter, createWebHashHistory } from 'vue-router';
import App from './App.vue';
import IssueView from './views/IssueView.vue';
import RecordsView from './views/RecordsView.vue';
import VerifyView from './views/VerifyView.vue';

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', redirect: '/issue' },
    { path: '/issue', name: 'issue', component: IssueView },
    { path: '/records', name: 'records', component: RecordsView },
    { path: '/verify', name: 'verify', component: VerifyView },
  ],
});

createApp(App).use(router).mount('#app');
