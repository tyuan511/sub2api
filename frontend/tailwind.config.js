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
        // 暖灰阶（温和圆润风格的浅色中性面，石灰色系）
        gray: {
          50: '#f8f6f3',
          100: '#f1eee9',
          200: '#e7e2da',
          300: '#d6cec2',
          400: '#a29a8e',
          500: '#78716c',
          600: '#57534e',
          700: '#44403c',
          800: '#292524',
          900: '#1c1917',
          950: '#141210'
        },
        // 暗色暖面（温和圆润风格的暖石色系，与 tokens.css 的 --fv-* 对齐）
        dark: {
          50: '#f5f5f4',
          100: '#e7e5e4',
          200: '#d6d3d1',
          300: '#a8a29e',
          400: '#8a8178',
          500: '#78716c',
          600: '#39342e',
          700: '#2b2722',
          800: '#1e1b18',
          900: '#191614',
          950: '#161412'
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
        glass: '0 6px 24px rgba(41, 37, 36, 0.08)',
        'glass-sm': '0 2px 12px rgba(41, 37, 36, 0.05)',
        glow: '0 10px 28px rgba(47, 111, 237, 0.22)',
        'glow-lg': '0 14px 36px rgba(47, 111, 237, 0.26)',
        card: '0 1px 2px rgba(41, 37, 36, 0.04)',
        'card-hover': '0 8px 24px rgba(41, 37, 36, 0.1)',
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
        md: '10px',
        lg: '10px',
        xl: '12px',
        '2xl': '16px',
        '3xl': '20px',
        '4xl': '2rem'
      }
    }
  },
  plugins: []
}
