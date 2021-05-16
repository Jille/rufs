/* eslint-disable no-alert */
import Vue from 'vue';
import { BootstrapVue, IconsPlugin } from 'bootstrap-vue';
import App from './App.vue';

import 'bootstrap/dist/css/bootstrap.css'
import 'bootstrap-vue/dist/bootstrap-vue.css'

Vue.use(BootstrapVue)
Vue.use(IconsPlugin)

const INFORM_MSG = 'Please report this bug, and include a description of what you were doing. If you can, please include a screenshot of the browser console.';

Vue.config.errorHandler = (err, vm, info) => {
  console.log('Unhandled Vue error: ', err, vm, info);
  alert(`An unhandled Vue error occurred. ${INFORM_MSG}`);
};
window.onerror = (msg, url, line, col, error) => {
  console.log('Unhandled window error event: ', msg, url, line, col, error);
  alert(`An unhandled window error occurred. ${INFORM_MSG}`);
};
window.addEventListener('unhandledrejection', (event) => {
  console.log('Unhandled rejection error: ', event);
  alert(`An unhandled promise rejection occurred. ${INFORM_MSG}`);
});
Vue.config.productionTip = false;

new Vue({
  render: (h) => h(App),
}).$mount('#app');