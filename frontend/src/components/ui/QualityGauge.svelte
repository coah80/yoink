<script>
  let { bitrate = 0 } = $props();

  let maxBitrate = 2000;
  let normalizedBitrate = $derived(Math.min(bitrate, maxBitrate));
  let angle = $derived(-90 + (normalizedBitrate / maxBitrate) * 180);
</script>

<div class="quality-gauge">
  <div class="quality-gauge-bg"></div>
  <div class="quality-gauge-mask"></div>
  <div class="quality-gauge-needle" style="transform: translateX(-50%) rotate({angle}deg)"></div>
  <div class="quality-gauge-center"></div>
  <div class="quality-gauge-value">
    <span>{bitrate}</span>
    <span class="quality-gauge-unit">Kbps</span>
  </div>
</div>

<style>
  .quality-gauge {
    position: relative;
    width: 180px;
    height: 100px;
    margin: 24px auto;
  }

  .quality-gauge-bg {
    position: absolute;
    width: 180px;
    height: 90px;
    border-radius: 90px 90px 0 0;
    background: conic-gradient(
      from 180deg at 50% 100%,
      var(--error) 0deg,
      var(--warning) 60deg,
      var(--success) 120deg,
      var(--success) 180deg
    );
    overflow: hidden;
  }

  .quality-gauge-mask {
    position: absolute;
    bottom: 0;
    left: 50%;
    transform: translateX(-50%);
    width: 130px;
    height: 65px;
    background: var(--surface);
    border-radius: 65px 65px 0 0;
  }

  .quality-gauge-needle {
    position: absolute;
    bottom: 0;
    left: 50%;
    width: 4px;
    height: 70px;
    background: white;
    border-radius: 2px;
    transform-origin: bottom center;
    transition: transform 0.5s ease-out;
    box-shadow: 0 0 10px rgba(255, 255, 255, 0.5);
  }

  .quality-gauge-center {
    position: absolute;
    bottom: -6px;
    left: 50%;
    transform: translateX(-50%);
    width: 16px;
    height: 16px;
    background: white;
    border-radius: 50%;
    box-shadow: 0 0 10px rgba(255, 255, 255, 0.3);
  }

  .quality-gauge-value {
    position: absolute;
    bottom: -40px;
    left: 50%;
    transform: translateX(-50%);
    font-family: var(--font-heading);
    font-size: 1.5rem;
    font-weight: 700;
    color: var(--text);
    white-space: nowrap;
  }

  .quality-gauge-unit {
    font-size: 0.875rem;
    font-weight: 500;
    color: var(--text-muted);
    margin-left: 4px;
  }
</style>
