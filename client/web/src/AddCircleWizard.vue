<template>
  <div>
    <Box title="Welcome to rufs." v-if="stage == 0">
      <p>Let's get you set up with your first circle. Please enter the information you received from your circle admin below:</p>
      <table class="table">
        <tbody>
          <tr>
            <td>Circle address</td>
            <td><input type="text" class="form-control" v-model="circle" placeholder="rufs.example.org"/></td>
          </tr>
          <tr>
            <td>Username</td>
            <td><input type="text" class="form-control" v-model="user"/></td>
          </tr>
          <tr>
            <td>Invite token</td>
            <td><input type="text" class="form-control" v-model="token"/></td>
          </tr>
          <tr>
            <td>CA certificate</td>
            <td><input type="text" class="form-control" v-model="ca"/></td>
          </tr>
        </tbody>
      </table>
      <div>
        <p>Or use OIDC if your provider is listed here:</p>
        <a :href="hashruIDM"><img src="https://idm.hashru.nl/pkg/img/logo-square.svg" width="60" /></a>
      </div>
      <div class="buttons">
        <p>rufs {{version }}</p>
        <button type="button" class="btn btn-primary" v-on:click="addCircle" v-bind:disabled="!canAddCircle()">
          Connect to circle
        </button>
      </div>
    </Box>
    <Box title="Adding circle..." v-if="stage == 1">
      <p v-if="error" class="error">{{ error }}</p>
      <center v-else><span class="icon-downloading"/></center>
    </Box>
    <Box title="Connected to circle!" v-if="stage == 2">
      <p v-if="error" class="error">{{ error }}</p>
      <p>If you want to share your own files with the circle, you can configure <em>shares</em>. Some shares may already exist,
      and you can expand them with files from a local directory you choose. You can also create a new share.</p>
      <table class="table">
        <thead>
          <tr>
            <th style="width: 50%">Share name in circle</th>
            <th style="width: 50%">Local directory</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="shares.length == 0">
            <td colspan="2">
              Nobody is sharing anything yet in this circle. Click the button below to share the first directory.
            </td>
          </tr>
          <tr v-for="(share, i) in shares" v-bind:key="i">
            <td>
              <template v-if="share.isNewShare">
                <div class="input-group">
                  <input type="text" v-bind:class="{'form-control': true, 'is-invalid': !isValidShareName(share.share)}" v-model="share.share" placeholder="Enter a public name"/>
                  <button type="button" class="btn btn-outline-danger" tabindex="-1" v-on:click="shares.splice(i, 1)">-</button>
                </div>
              </template>
              <template v-else>
                {{ share.share }}
              </template>
            </td>
            <td>
              <div class="input-group">
                <input type="text" class="form-control" v-model="share.localPath" placeholder="Enter a local path to share it"/>
              </div>
            </td>
          </tr>
        </tbody>
        <tfoot>
          <tr>
            <td colspan="2">
              <button type="button" class="btn btn-success" v-on:click="addNewShare">
                <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" fill="currentColor" class="bi bi-plus" viewBox="0 0 16 16">
                  <path d="M8 4a.5.5 0 0 1 .5.5v3h3a.5.5 0 0 1 0 1h-3v3a.5.5 0 0 1-1 0v-3h-3a.5.5 0 0 1 0-1h3v-3A.5.5 0 0 1 8 4z"/>
                </svg>
                Add a share
              </button>
            </td>
          </tr>
        </tfoot>
      </table>
      <div class="buttons">
        <p>rufs {{version }}</p>
        <button type="button" class="btn btn-primary" v-on:click="saveShares" v-bind:disabled="!canSaveShares">
          {{ anythingShared ? "Save and continue" : "Continue without sharing" }}
        </button>
      </div>
    </Box>
    <Box title="Saving changes..." v-if="stage == 3">
      <p v-if="error" class="error">{{ error }}</p>
      <center v-else><span class="icon-downloading"/></center>
    </Box>
    <Box title="You're all done!" v-if="stage == 4">
      <p>All files you share, and files shared with you, should now be visible in your rufs directory.</p>
      <div class="buttons">
        <p>rufs {{version }}</p>
        <button type="button" class="btn btn-primary" v-on:click="closeWindow" v-if="!closeFailed">
          Close
        </button>
        <p v-if="closeFailed" class="error">
          Looks like that close button didn't work. Go ahead and close this tab yourself.
        </p>
      </div>
    </Box>
  </div>
</template>

<script lang="ts">
import { Component, Vue, Prop } from 'vue-property-decorator';
import { RufsConfig, RufsService } from './rufs-service';
import Box from './Box.vue';

interface ShareLocalPath {
  share: string;
  localPath: string;
  isNewShare: boolean;
}

@Component({
  components: {
    Box,
  }
})
export default class AddCircleWizard extends Vue {
  @Prop() private version!: string;
  @Prop() private config!: RufsConfig;

  private circle = "";
  private user = "";
  private token = "";
  private ca = "";

  private hostname = "";

  private stage = 0;
  private error = "";
  private shares: ShareLocalPath[] = [];

  private closeFailed = false;

  private async beforeMount() {
    const urlParams = new URLSearchParams(window.location.search);
    this.circle = urlParams.get('circle') || '';
    this.user = urlParams.get('username') || '';
    this.token = urlParams.get('token') || '';
    this.ca = urlParams.get('fingerprint') || '';

    RufsService.getHostname().then((h) => { this.hostname = h}).catch(() => {});
  }

  public canAddCircle(): boolean {
    return !!this.circle && !!this.user && !!this.token && !!this.ca;
  }

  public isValidShareName(name: string): boolean {
    return new RegExp('^[a-zA-Z0-9 _.-]+$').test(name);
  }

  public get anythingShared(): boolean {
    for(let i = 0; i < this.shares.length; i += 1) {
      if (this.shares[i].localPath != "" || this.shares[i].isNewShare) {
        return true;
      }
    }
    return false;
  }

  public get canSaveShares(): boolean {
    for(let i = 0; i < this.shares.length; i += 1) {
      if (this.shares[i].isNewShare && (this.shares[i].share == "" || this.shares[i].localPath == "")) {
        return false;
      }
    }
    return true;
  }

  private async addCircle(): Promise<void> {
    this.stage = 1;
    try {
      await RufsService.register(this.circle, this.user, this.token, this.ca);
      let shares = await RufsService.sharesInCircle(this.circle);
      this.shares = [];
      shares.forEach((share) => {
        this.shares.push({share, localPath: "", isNewShare: false});
      });
      this.stage = 2;
    } catch(e) {
      this.error = e.message;
    }
  }

  private addNewShare() {
    this.shares.push({share: "", localPath: "", isNewShare: true});
  }

  private async saveShares(): Promise<void> {
    this.stage = 3;
    try {
      for (var i = 0; i < this.shares.length; ++i) {
        const share = this.shares[i];
        if (share.localPath != "") {
          await RufsService.addShare(this.circle, share.share, share.localPath);
        }
      }
      await RufsService.openExplorer();
      this.stage = 4;
    } catch(e) {
      this.error = e.message;
    }
  }

  private closeWindow() {
    window.close();
    setTimeout(() => {window.close();}, 500);
    setTimeout(() => {window.close();}, 1000);
    setTimeout(() => {window.close();}, 1500);
    setTimeout(() => {
      this.closeFailed = true;
      window.close();
    }, 2000);
  }

  get hashruIDM() {
    return 'https://rufs.hashru.nl/?hostname=' + encodeURIComponent(this.hostname) +'&return_url=' + encodeURIComponent(location.href);
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

.buttons {
  display: flex;
  justify-content: flex-end;
  align-items: flex-end;
  p {
    flex-grow: 1;
    margin: 0;
    color: var(--bs-secondary);
  }
}
</style>
