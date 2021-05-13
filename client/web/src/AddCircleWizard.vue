<template>
  <div>
    <Box title="Welcome to rufs." v-if="stage == 0">
      <form v-on:submit.prevent="addCircle">
        <p>Let's get you set up with your first circle. Please enter the information you received from your circle admin below:</p>
        <p>Circle address: <input type="text" ref="circle"/></p>
        <p>Username: <input type="text" ref="user"/></p>
        <p>Invite token: <input type="text" ref="token"/></p>
        <p>CA certificate: <input type="text" ref="ca"/></p>
        <p>
          <button type="submit">Add circle</button>
        </p>
      </form>
    </Box>
    <Box title="Adding circle..." v-if="stage == 1">
      <p v-if="error" class="error">{{ error }}</p>
      <center v-else><span class="icon-downloading"/></center>
    </Box>
    <Box title="Connected to circle!" v-if="stage == 2">
    </Box>
  </div>
</template>

<script lang="ts">
import { Component, Vue, Prop } from 'vue-property-decorator';
import { RufsConfig, RufsService } from './rufs-service';
import Box from './Box.vue';

@Component({
  components: {
    Box,
  }
})
export default class AddCircleWizard extends Vue {
  @Prop() private config!: RufsConfig;

  private stage = 0;
  private error = "";

  private async addCircle(): Promise<void> {
    const circle = (this.$refs.circle as HTMLInputElement).value;
    const user = (this.$refs.user as HTMLInputElement).value;
    const token = (this.$refs.token as HTMLInputElement).value;
    const ca = (this.$refs.ca as HTMLInputElement).value;

    this.stage = 1;
    try {
      await RufsService.register(circle, user, token, ca);
      this.stage = 2;
    } catch(e) {
      this.error = e.message;
      return;
    }
  }
}
</script>

<style lang="scss">
body {
  background-color: var(--bs-light) !important;
  padding-top: 4em;
}

span.icon-downloading {
  display: inline-block;
  background-image: url("./spinner.svg");
  width: 96px;
  height: 96px;
  margin-top: 30px;
}

.error {
  color: red;
}
</style>
