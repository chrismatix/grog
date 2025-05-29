import React from 'react';
import { render, screen } from '@testing-library/react';
import { Link } from '../src/Link';

describe('Link Component', () => {
  test('renders with required props', () => {
    render(<Link href="https://example.com">Example Link</Link>);
    const link = screen.getByRole('link', { name: /example link/i });
    expect(link).toBeInTheDocument();
    expect(link).toHaveAttribute('href', 'https://example.com');
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', 'noopener noreferrer');
  });

  test('applies custom className when provided', () => {
    render(
      <Link href="https://example.com" className="custom-class">
        Example Link
      </Link>
    );
    const link = screen.getByRole('link', { name: /example link/i });
    expect(link.className).toContain('custom-class');
  });

  test('uses custom target when provided', () => {
    render(
      <Link href="https://example.com" target="_self">
        Example Link
      </Link>
    );
    const link = screen.getByRole('link', { name: /example link/i });
    expect(link).toHaveAttribute('target', '_self');
  });

  test('uses custom rel when provided', () => {
    render(
      <Link href="https://example.com" rel="alternate">
        Example Link
      </Link>
    );
    const link = screen.getByRole('link', { name: /example link/i });
    expect(link).toHaveAttribute('rel', 'alternate');
  });

  test('renders icon when provided', () => {
    const iconTestId = 'test-icon';
    render(
      <Link
        href="https://example.com"
        icon={<span data-testid={iconTestId}>Icon</span>}
      >
        Example Link
      </Link>
    );
    expect(screen.getByTestId(iconTestId)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /example link/i })).toContainElement(
      screen.getByTestId(iconTestId)
    );
  });

  test('renders children correctly', () => {
    render(
      <Link href="https://example.com">
        <span data-testid="child">Child Element</span>
      </Link>
    );
    expect(screen.getByTestId('child')).toBeInTheDocument();
    expect(screen.getByTestId('child')).toHaveTextContent('Child Element');
  });
});
