<template>
  <div id="app">
    <Box v-if="error" title="Error">
      {{ error }}
    </Box>

    <Box v-else-if="!config" title="Loading..."/>

    <AddCircleWizard v-else-if="config.circles.length == 0" v-bind:config="config"/>

    <Box v-else title="You're all set up!">
      You can close this tab now.
    </Box>
  </div>
</template>

<script lang="ts">
import { Component, Vue } from 'vue-property-decorator';
import { RufsConfig, RufsService } from './rufs-service';
import Box from './Box.vue';
import AddCircleWizard from './AddCircleWizard.vue';

@Component({
  components: {
    Box,
    AddCircleWizard,
  }
})
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
body {
  background-color: var(--bs-light) !important;
  padding-top: 4em;
}
</style>
