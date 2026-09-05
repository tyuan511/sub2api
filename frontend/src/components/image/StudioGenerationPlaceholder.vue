<template>
  <div class="generation-placeholder" :class="{ 'generation-wide': aspect > 1.2, 'generation-panorama': aspect > 2.2 }" :style="{ aspectRatio: aspect, '--phase': `${-index * 1.7}s` }">
    <div class="generation-atmosphere" aria-hidden="true">
      <span class="aurora aurora-violet" /><span class="aurora aurora-blue" /><span class="aurora aurora-rose" />
      <span class="generation-grid" /><span class="generation-sheen" />
    </div>
    <span class="generation-badge"><i aria-hidden="true" />{{ t('imageStudio.generating') }}</span>
    <span class="generation-frame" aria-hidden="true" />
    <div class="generation-content">
      <div class="generation-universe" aria-hidden="true">
        <span class="universe-halo" />
        <div class="orbit-plane orbit-one"><span class="orbit-track" /><span class="orbit-traveler"><i /></span></div>
        <div class="orbit-plane orbit-two"><span class="orbit-track" /><span class="orbit-traveler"><i /></span></div>
        <div class="orbit-plane orbit-three"><span class="orbit-track" /><span class="orbit-traveler"><i /></span></div>
        <div class="generation-core">
          <span class="core-light" />
          <svg class="core-spark" viewBox="0 0 64 64" fill="none">
            <path d="M32 9C35 24 40 29 55 32C40 35 35 40 32 55C29 40 24 35 9 32C24 29 29 24 32 9Z" fill="currentColor" />
            <path d="M51 5C52 11 54 13 60 14C54 15 52 17 51 23C50 17 48 15 42 14C48 13 50 11 51 5Z" fill="currentColor" opacity=".7" />
          </svg>
        </div>
        <span v-for="particle in 6" :key="particle" class="generation-particle" :style="{ '--particle': particle }"><i /></span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
const props = defineProps<{ ratio: string; index: number }>()
const aspect = computed(() => {
  const [width, height] = props.ratio.split(':').map(Number)
  return width > 0 && height > 0 ? width / height : 1
})
const { t } = useI18n()
</script>

<style scoped>
.generation-placeholder {
  position: relative;
  isolation: isolate;
  container-type: inline-size;
  display: grid;
  place-items: center;
  width: 100%;
  min-width: 0;
  min-height: 190px;
  overflow: hidden;
  border: 1px solid color-mix(in srgb, var(--studio-accent) 15%, var(--studio-line));
  border-radius: 12px;
  background: var(--studio-surface);
  color: var(--studio-ink);
}
.generation-atmosphere { position: absolute; inset: 0; z-index: -1; overflow: hidden; pointer-events: none; }
.aurora { position: absolute; width: 85%; aspect-ratio: 1; border-radius: 50%; filter: blur(32px); opacity: .28; animation: aurora-drift 12s ease-in-out infinite alternate; animation-delay: var(--phase); }
.aurora-violet { left: -26%; top: -30%; background: radial-gradient(circle, #a88bfa, transparent 70%); }
.aurora-blue { right: -28%; top: 14%; background: radial-gradient(circle, #78d8f4, transparent 70%); animation-delay: calc(var(--phase) - 4s); animation-direction: alternate-reverse; }
.aurora-rose { left: 4%; bottom: -48%; background: radial-gradient(circle, #e7a6ed, transparent 70%); animation-delay: calc(var(--phase) - 8s); }
.generation-grid { position: absolute; inset: 0; background-image: linear-gradient(var(--studio-accent) 1px, transparent 1px), linear-gradient(90deg, var(--studio-accent) 1px, transparent 1px); background-size: 28px 28px; opacity: .07; mask-image: radial-gradient(ellipse, #000, transparent 72%); }
.generation-sheen { position: absolute; inset: -100%; background: linear-gradient(115deg, transparent 42%, #c4b5fd24 49%, #fff7 50%, transparent 58%); animation: sheen-drift 9s ease-in-out infinite; animation-delay: var(--phase); }
.generation-badge { position: absolute; left: 16px; top: 15px; display: inline-flex; align-items: center; gap: 6px; color: var(--studio-accent); font-size: 10px; letter-spacing: .04em; }
.generation-badge i { width: 5px; height: 5px; border-radius: 50%; background: currentColor; box-shadow: 0 0 8px currentColor; animation: signal-breathe 2.6s ease-in-out infinite; }
.generation-frame { position: absolute; inset: 13px; border: 1px solid color-mix(in srgb, var(--studio-accent) 22%, transparent); border-radius: 4px; mask: linear-gradient(#000, #000) left top / 10px 10px no-repeat, linear-gradient(#000, #000) right top / 10px 10px no-repeat, linear-gradient(#000, #000) left bottom / 10px 10px no-repeat, linear-gradient(#000, #000) right bottom / 10px 10px no-repeat; pointer-events: none; opacity: .6; }
.generation-content { position: absolute; inset: 0; display: grid; place-items: center; padding: 24px; }
.generation-universe { position: relative; width: clamp(104px, 46cqw, 212px); aspect-ratio: 1; }
.universe-halo { position: absolute; inset: -20%; border-radius: 50%; background: radial-gradient(circle, #a99bf43d, #bac8ff1c 42%, transparent 68%); animation: halo-breathe 5s ease-in-out infinite; animation-delay: var(--phase); }
.orbit-plane { position: absolute; inset: 4%; border-radius: 50%; }
.orbit-one { transform: rotate(-32deg) scaleY(.57); }
.orbit-two { transform: rotate(45deg) scaleY(.64); }
.orbit-three { inset: 12%; transform: rotate(95deg) scaleY(.82); }
.orbit-track { position: absolute; inset: 0; border: 1px solid #a799ec55; border-radius: inherit; box-shadow: 0 0 12px #b4a5fc12; }
.orbit-track::after { content: ''; position: absolute; inset: -1px; border-radius: inherit; background: conic-gradient(from 20deg, transparent 55%, #9d81f033 72%, #a998f8 93%, #fff 98%, transparent); mask-image: radial-gradient(closest-side, transparent calc(100% - 2px), #000 calc(100% - 1px)); animation: orbit-spin 7s linear infinite; animation-delay: var(--phase); }
.orbit-traveler { position: absolute; inset: 0; animation: orbit-spin 7s linear infinite; animation-delay: var(--phase); }
.orbit-traveler i { position: absolute; left: calc(50% - 3px); top: -2px; width: 6px; height: 6px; border-radius: 50%; background: #fff; box-shadow: 0 0 4px #fff, 0 0 10px #8d70f9, 0 0 18px #ac9aff; }
.orbit-two .orbit-track::after, .orbit-two .orbit-traveler { animation-duration: 10s; animation-direction: reverse; }
.orbit-two .orbit-track { border-color: #84cbe84d; }
.orbit-two .orbit-traveler i { box-shadow: 0 0 4px #fff, 0 0 10px #73cfe6; }
.orbit-three .orbit-track::after, .orbit-three .orbit-traveler { animation-duration: 13s; }
.generation-core { position: absolute; inset: 27%; display: grid; place-items: center; overflow: hidden; border-radius: 32%; transform: rotate(-12deg); border: 1px solid #fff9; background: radial-gradient(circle at 30% 20%, #e1d7ff 0%, #af96f4 30%, #8170dd 70%, #6853c4); box-shadow: inset 0 2px 12px #fff7, inset 0 -5px 14px #6952bc55, 0 10px 26px #8a75d933, 0 0 28px #b7a3ff33; animation: core-float 6s ease-in-out infinite; animation-delay: var(--phase); }
.core-light { position: absolute; inset: -70%; background: conic-gradient(from 30deg, transparent, #b4eaffaa, transparent 38%, #f5cfff88, transparent 70%); filter: blur(8px); animation: orbit-spin 9s linear infinite; animation-delay: var(--phase); }
.core-spark { position: relative; width: 64%; color: #fff; filter: drop-shadow(0 0 5px #fff8); transform: rotate(12deg); }
.generation-particle { position: absolute; inset: -2%; transform: rotate(calc(var(--particle) * 60deg)); }
.generation-particle i { position: absolute; left: 50%; top: 0; width: 3px; height: 3px; border-radius: 50%; background: var(--studio-accent); opacity: .4; box-shadow: 0 0 6px #af95ff66; animation: particle-drift 4.8s ease-in-out infinite; animation-delay: calc(var(--phase) - var(--particle) * .7s); }
.generation-particle:nth-last-child(even) { inset: 10%; }
.generation-wide .generation-universe { width: clamp(84px, 29cqw, 140px); }
.generation-panorama .generation-universe { max-width: 100px; }
@keyframes orbit-spin { to { transform: rotate(360deg); } }
@keyframes aurora-drift { to { transform: translate(22%, 16%) scale(1.2); } }
@keyframes sheen-drift { 0%, 15% { transform: translateX(-35%); opacity: 0; } 40% { opacity: .6; } 75%, 100% { transform: translateX(35%); opacity: 0; } }
@keyframes halo-breathe { 50% { transform: scale(1.13); opacity: .55; } }
@keyframes core-float { 50% { transform: translateY(-6px) rotate(8deg); } }
@keyframes signal-breathe { 50% { opacity: .35; } }
@keyframes particle-drift { 50% { transform: translate(6px, -8px); opacity: .9; } }
@container (max-width: 250px) {
  .generation-badge { top: 12px; left: 12px; font-size: 9px; }
}
@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after { animation: none !important; }
  .generation-sheen { display: none; }
}
</style>
