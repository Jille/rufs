<template>
  <div class="box">
    <h1 v-if="title">{{title}}</h1>
    <slot/>
  </div>
</template>

<script lang="ts">
import { Component, Prop, Vue } from 'vue-property-decorator';

@Component
export default class Box extends Vue {
  @Prop() public title!: string;
}
</script>

<style scoped lang="scss">
.box {
  width: 800px;
  text-align: left;
  margin: 30px auto;
  border: 2px solid var(--bs-dark);
  padding: 10px;
  background-color: var(--bs-light);
  color: var(--bs-dark);
  box-shadow: 0px 1px 10px var(--bs-dark);
  border-radius: 0.35em;

  h1 {
    background: linear-gradient(180deg, rgba(0, 0, 0, 0.5), rgba(255, 255, 255, 0));
    text-align: center;
    padding: 10px;
    margin: -10px;
  }
}
</style>