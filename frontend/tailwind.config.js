/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        // FastVibe brand blue
        primary: {
          50: '#f1f5ff',
          100: '#dfe8ff',
          200: '#c7d7ff',
          300: '#9fbbff',
          400: '#7ea6ff',
          500: '#2f6fed',
          600: '#245bcc',
          700: '#1e4aa7',
          800: '#1d4087',
          900: '#1d376d',
          950: '#17274c'
        },
        // 辅助色 - 深蓝灰（中性阶）
        accent: {
          50: '#f8fafc',
          100: '#f1f5f9',
          200: '#e2e8f0',
          300: '#cbd5e1',
          400: '#94a3b8',
          500: '#64748b',
          600: '#475569',
          700: '#334155',
          800: '#1e293b',
          900: '#0f172a',
          950: '#020617'
        },
        // 冷灰阶（玻璃风格的浅色中性面）
        gray: {
          50: '#f2f6fc',
          100: '#e8eef6',
          200: '#d7e0ec',
          300: '#b6c2d4',
          400: '#8fa0b5',
          500: '#64748b',
          600: '#475569',
          700: '#334155',
          800: '#1e293b',
          900: '#111827',
          950: '#0c0f14'
        },
        // 暗色玻璃面（中性近黑、带一丝冷灰，与 tokens.css 的 --fv-* 对齐）
        dark: {
          50: '#ecedf1',
          100: '#d6d9df',
          200: '#b9bdc6',
          300: '#979ca7',
          400: '#82868f',
          500: '#62666e',
          600: '#2e2e36',
          700: '#1e1e25',
          800: '#151519',
          900: '#0f0f13',
          950: '#0a0a0d'
        },
        // 语义令牌：跟随亮暗模式自动切换（映射 styles/tokens.css 的 --fv-* 变量）。
        // 注意：纯 CSS 变量色值不支持 Tailwind 的透明度修饰符（bg-surface/50），
        // 需要透明度时请用具体色阶（primary-N / dark-N）或 color-mix。
        surface: {
          DEFAULT: 'var(--fv-surface)',
          2: 'var(--fv-surface-2)',
          3: 'var(--fv-surface-3)',
          solid: 'var(--fv-surface-solid)'
        },
        line: {
          DEFAULT: 'var(--fv-line)',
          soft: 'var(--fv-line-soft)'
        },
        content: {
          DEFAULT: 'var(--fv-text)',
          soft: 'var(--fv-text-soft)',
          muted: 'var(--fv-muted)'
        }
      },
      fontFamily: {
        sans: [
          'Noto Sans SC',
          'system-ui',
          '-apple-system',
          'BlinkMacSystemFont',
          'Segoe UI',
          'Roboto',
          'Helvetica Neue',
          'Arial',
          'PingFang SC',
          'Hiragino Sans GB',
          'Microsoft YaHei',
          'sans-serif'
        ],
        mono: ['DM Mono', 'SFMono-Regular', 'Menlo', 'Monaco', 'Consolas', 'monospace']
      },
      boxShadow: {
        glass: '0 6px 24px rgba(23, 58, 120, 0.10)',
        'glass-sm': '0 2px 12px rgba(23, 58, 120, 0.06)',
        glow: '0 10px 28px rgba(47, 111, 237, 0.25)',
        'glow-lg': '0 14px 36px rgba(47, 111, 237, 0.28)',
        card: '0 1px 2px rgba(23, 58, 120, 0.04)',
        'card-hover': '0 12px 32px rgba(23, 58, 120, 0.14)',
        'inner-glow': 'inset 0 1px 0 rgba(255, 255, 255, 0.1)'
      },
      backgroundImage: {
        'gradient-radial': 'radial-gradient(var(--tw-gradient-stops))',
        'gradient-primary': 'linear-gradient(135deg, #2f6fed 0%, #245bcc 100%)',
        'gradient-dark': 'linear-gradient(135deg, #1e293b 0%, #0f172a 100%)',
        'gradient-glass':
          'linear-gradient(135deg, rgba(255,255,255,0.1) 0%, rgba(255,255,255,0.05) 100%)',
        'mesh-gradient': 'none'
      },
      animation: {
        'fade-in': 'fadeIn 0.3s ease-out',
        'slide-up': 'slideUp 0.3s ease-out',
        'slide-down': 'slideDown 0.3s ease-out',
        'slide-in-right': 'slideInRight 0.3s ease-out',
        'scale-in': 'scaleIn 0.2s ease-out',
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        shimmer: 'shimmer 2s linear infinite',
        glow: 'glow 2s ease-in-out infinite alternate'
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: '0' },
          '100%': { opacity: '1' }
        },
        slideUp: {
          '0%': { opacity: '0', transform: 'translateY(10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideDown: {
          '0%': { opacity: '0', transform: 'translateY(-10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideInRight: {
          '0%': { opacity: '0', transform: 'translateX(20px)' },
          '100%': { opacity: '1', transform: 'translateX(0)' }
        },
        scaleIn: {
          '0%': { opacity: '0', transform: 'scale(0.95)' },
          '100%': { opacity: '1', transform: 'scale(1)' }
        },
        shimmer: {
          '0%': { backgroundPosition: '-200% 0' },
          '100%': { backgroundPosition: '200% 0' }
        },
        glow: {
          '0%': { boxShadow: '0 10px 24px rgba(47, 111, 237, 0.14)' },
          '100%': { boxShadow: '0 12px 30px rgba(47, 111, 237, 0.2)' }
        }
      },
      backdropBlur: {
        xs: '2px',
        glass: '16px',
        'glass-strong': '24px'
      },
      borderRadius: {
        DEFAULT: '8px',
        sm: '8px',
        md: '9px',
        lg: '9px',
        xl: '10px',
        '2xl': '14px',
        '3xl': '18px',
        '4xl': '2rem'
      }
    }
  },
  plugins: []
}
