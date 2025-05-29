import { colors } from '@monorepo/theme';
import Header from './components/Header';
import Footer from './components/Footer';
import { isNotEmpty } from '@monorepo/utils';

export default function Home() {
  // Example of using the utility function
  const welcomeMessage = "Welcome to our monorepo example!";
  console.log(`Welcome message is not empty: ${isNotEmpty(welcomeMessage)}`);

  return (
    <div className="grid grid-rows-[20px_1fr_20px] items-center justify-items-center min-h-screen p-8 pb-20 gap-16 sm:p-20 font-[family-name:var(--font-geist-sans)]">
      <main className="flex flex-col gap-[32px] row-start-2 items-center sm:items-start">
        <Header />

        <div className="mt-8 p-6 rounded-lg bg-primary-50 border border-primary-200">
          <h2 className="text-xl font-bold mb-4" style={{ color: colors.primary[700] }}>
            Monorepo Features
          </h2>
          <ol className="list-inside list-decimal text-sm/6 text-center sm:text-left font-[family-name:var(--font-geist-mono)]">
            <li className="mb-2 tracking-[-.01em]">
              Shared UI components from{" "}
              <code className="bg-black/[.05] dark:bg-white/[.06] px-1 py-0.5 rounded font-[family-name:var(--font-geist-mono)] font-semibold">
                @monorepo/ui-components
              </code>
            </li>
            <li className="mb-2 tracking-[-.01em]">
              Shared utilities from{" "}
              <code className="bg-black/[.05] dark:bg-white/[.06] px-1 py-0.5 rounded font-[family-name:var(--font-geist-mono)] font-semibold">
                @monorepo/utils
              </code>
            </li>
            <li className="tracking-[-.01em]">
              Shared theme from{" "}
              <code className="bg-black/[.05] dark:bg-white/[.06] px-1 py-0.5 rounded font-[family-name:var(--font-geist-mono)] font-semibold">
                @monorepo/theme
              </code>
            </li>
          </ol>
        </div>
      </main>

      <div className="row-start-3">
        <Footer />
      </div>
    </div>
  );
}
