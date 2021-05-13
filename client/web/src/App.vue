<template>
  <div>
    <p v-if="error">{{ error }}</p>
    <p>Currently active configuration:</p>
    <pre>{{ JSON.stringify(config, null, true) }}</pre>
  </div>
</template>

<script lang="ts">
import { Component, Vue } from 'vue-property-decorator';
import { RufsConfig, RufsService } from './rufs-service';

@Component
export default class App extends Vue {
  private config: RufsConfig | null = null;
  private error = "";

  private async mounted(): Promise<void> {
    try {
      this.config = await RufsService.getConfig();
    } catch(e) {
      this.error = 'error' in e ? e.error : e.message;
    }
  }
}
</script>

<style lang="scss">
</style>